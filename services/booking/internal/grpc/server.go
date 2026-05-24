package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	bookingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/booking/v1"
	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"
	userv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/user/v1"

	bookingdb "github.com/ecopoint/ecopoint/services/booking/db/sqlc"
)

// Phí Vận Hành mặc định khi Vựa nhận đơn (Master Doc §1).
const stationAcceptFeeEP int64 = 20

// TTL của PIN do User cấp cho Driver — Master Doc §4.1.
const pinTTL = 10 * time.Minute

// Tỷ lệ điểm User nhận theo kg + flat điểm Driver — công thức tạm V1.3.
const (
	userRewardPerKg    int64 = 10
	driverRewardFlat   int64 = 20
	weightToleranceMax       = 0.10 // 10% — quá → CANCELLED + DeductTrustScore
	trustPenalty       int32 = 20   // điểm trust bị trừ khi gian lận
)

type BookingServer struct {
	bookingv1.UnimplementedBookingServiceServer
	pool        *pgxpool.Pool
	q           *bookingdb.Queries
	pointClient pointv1.PointServiceClient
	userClient  userv1.UserServiceClient
	log         *slog.Logger
}

func NewBookingServer(
	pool *pgxpool.Pool,
	pointClient pointv1.PointServiceClient,
	userClient userv1.UserServiceClient,
	log *slog.Logger,
) *BookingServer {
	return &BookingServer{
		pool:        pool,
		q:           bookingdb.New(pool),
		pointClient: pointClient,
		userClient:  userClient,
		log:         log.With("component", "booking-server"),
	}
}

// ============================================================
// CreateBooking — sinh PIN 4 số + TTL 10 phút, trả về cho User.
// ============================================================
func (s *BookingServer) CreateBooking(ctx context.Context, req *bookingv1.CreateBookingRequest) (*bookingv1.CreateBookingResponse, error) {
	address := strings.TrimSpace(req.GetAddress())
	if address == "" {
		return nil, status.Error(codes.InvalidArgument, "address is required")
	}
	customerID, err := uuid.Parse(req.GetCustomerId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid customer_id: %v", err)
	}
	if req.GetLatitude() == 0 && req.GetLongitude() == 0 {
		return nil, status.Error(codes.InvalidArgument, "latitude/longitude are required")
	}

	estimatedKg := decimal.Zero
	if v := req.GetEstimatedKg().GetValue(); v != "" {
		estimatedKg, err = decimal.NewFromString(v)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid estimated_kg: %v", err)
		}
	}

	material, ok := protoMaterialToDB[req.GetMaterialType()]
	if !ok {
		material = bookingdb.MaterialTypeMixed
	}

	scheduledAt := pgtype.Timestamptz{}
	if t := req.GetScheduledAt(); t != nil {
		scheduledAt = pgtype.Timestamptz{Time: t.AsTime(), Valid: true}
	}

	// V1.3: generate PIN cryptographic random + set TTL 10 phút.
	pin, err := generatePIN4()
	if err != nil {
		s.log.Error("PIN gen failed", "err", err.Error())
		return nil, status.Errorf(codes.Internal, "pin generation failed: %v", err)
	}
	pinExpire := pgtype.Timestamptz{Time: time.Now().Add(pinTTL).UTC(), Valid: true}

	row, err := s.q.CreateBooking(ctx, bookingdb.CreateBookingParams{
		CustomerID:    toPgUUID(customerID),
		Address:       address,
		StMakepoint:   req.GetLongitude(),
		StMakepoint_2: req.GetLatitude(),
		EstimatedKg:   toPgNumeric(estimatedKg),
		MaterialType:  material,
		Note:          ptrString(strings.TrimSpace(req.GetNote())),
		ScheduledAt:   scheduledAt,
		PinCode:       &pin,
		PinExpiredAt:  pinExpire,
	})
	if err != nil {
		s.log.Error("create booking failed", "err", err.Error())
		return nil, status.Errorf(codes.Internal, "create failed: %v", err)
	}

	s.log.Info("booking created",
		"booking_id", fromPgUUID(row.ID),
		"customer_id", req.GetCustomerId(),
		"address", address,
		"pin_expires_at", pinExpire.Time.Format(time.RFC3339),
	)

	return &bookingv1.CreateBookingResponse{
		Booking: toProtoBooking(bookingFields{
			ID: row.ID, CustomerID: row.CustomerID, CollectorID: row.CollectorID,
			Status: row.Status, Address: row.Address,
			Longitude: row.Longitude, Latitude: row.Latitude,
			EstimatedKg: row.EstimatedKg, MaterialType: row.MaterialType,
			Note: row.Note, ScheduledAt: row.ScheduledAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			StationID: row.StationID, PinCode: row.PinCode, PinExpiredAt: row.PinExpiredAt,
		}),
	}, nil
}

// ============================================================
// CollectorAcceptBooking — Vựa nhận đơn → trừ 20 EP phí.
//
// Saga 2 bước:
//
//	1) Atomic reserve booking: UPDATE WHERE status='pending'.
//	   → 0 row = Vựa khác grab trước → trả FailedPrecondition.
//	2) Call PointService.DeductFee(20 EP, idem=accept-<booking_id>).
//	   → Fail (insufficient available) = compensation rollback
//	     booking về 'pending'.
//
// Idempotent: nếu booking đã ở 'accepted' bởi cùng station_id, replay
// trả về thành công không gọi lại PointService.
// ============================================================
func (s *BookingServer) CollectorAcceptBooking(ctx context.Context, req *bookingv1.CollectorAcceptBookingRequest) (*bookingv1.CollectorAcceptBookingResponse, error) {
	bookingID, err := uuid.Parse(req.GetBookingId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid booking_id: %v", err)
	}
	stationID, err := uuid.Parse(req.GetStationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid station_id: %v", err)
	}

	// Lookup station — owner_id sẽ là chủ ví bị trừ 20 EP.
	station, err := s.q.GetStation(ctx, toPgUUID(stationID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "station not found")
		}
		return nil, status.Errorf(codes.Internal, "lookup station: %v", err)
	}
	if !station.IsActive {
		return nil, status.Error(codes.FailedPrecondition, "station is inactive")
	}

	// Idempotent check — đã accept by same station thì replay không gọi DeductFee.
	existing, err := s.q.GetBookingByID(ctx, toPgUUID(bookingID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "booking not found")
		}
		return nil, status.Errorf(codes.Internal, "lookup booking: %v", err)
	}
	if existing.Status == bookingdb.BookingStatusAccepted &&
		existing.StationID.Valid && existing.StationID == toPgUUID(stationID) {
		return &bookingv1.CollectorAcceptBookingResponse{
			Booking: toProtoBooking(bookingFields{
				ID: existing.ID, CustomerID: existing.CustomerID, CollectorID: existing.CollectorID,
				Status: existing.Status, Address: existing.Address,
				Longitude: existing.Longitude, Latitude: existing.Latitude,
				EstimatedKg: existing.EstimatedKg, MaterialType: existing.MaterialType,
				Note: existing.Note, ScheduledAt: existing.ScheduledAt,
				CreatedAt: existing.CreatedAt, UpdatedAt: existing.UpdatedAt,
				StationID: existing.StationID, PinCode: nil, PinExpiredAt: existing.PinExpiredAt,
			}),
		}, nil
	}

	// ─── STEP 1: Atomic reserve ───────────────────────────────
	row, err := s.q.AcceptBookingByStation(ctx, bookingdb.AcceptBookingByStationParams{
		ID:        toPgUUID(bookingID),
		StationID: toPgUUID(stationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.FailedPrecondition, "booking is not pending (already accepted or cancelled)")
		}
		return nil, status.Errorf(codes.Internal, "reserve booking: %v", err)
	}

	// ─── STEP 2: DeductFee 20 EP từ ví owner của Vựa ──────────
	idemKey := fmt.Sprintf("accept-%s", bookingID.String())
	deductCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	deductResp, deductErr := s.pointClient.DeductFee(deductCtx, &pointv1.DeductFeeRequest{
		UserId:         fromPgUUID(station.OwnerID),
		Amount:         stationAcceptFeeEP,
		Reason:         "station_fee_20ep",
		ReferenceId:    bookingID.String(),
		IdempotencyKey: idemKey,
	})

	if deductErr != nil {
		// ─── COMPENSATION: rollback booking về pending ─────────
		compCtx, compCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer compCancel()

		if rbErr := s.q.RollbackBookingAccept(compCtx, bookingdb.RollbackBookingAcceptParams{
			ID:        toPgUUID(bookingID),
			StationID: toPgUUID(stationID),
		}); rbErr != nil {
			s.log.Error("compensation FAILED — manual reconcile",
				"booking_id", bookingID, "station_id", stationID,
				"cause", deductErr.Error(), "rb_err", rbErr.Error(),
			)
		} else {
			s.log.Warn("CollectorAcceptBooking compensated",
				"booking_id", bookingID, "station_id", stationID,
				"cause", deductErr.Error(),
			)
		}

		if st, ok := status.FromError(deductErr); ok {
			// FailedPrecondition (insufficient balance) → 400 cho FE.
			return nil, status.Errorf(st.Code(), "deduct fee failed: %s", st.Message())
		}
		return nil, status.Errorf(codes.Internal, "deduct fee failed: %v", deductErr)
	}

	s.log.Info("booking accepted",
		"booking_id", bookingID, "station_id", stationID,
		"fee_ep", stationAcceptFeeEP,
		"point_tx_id", deductResp.GetTransaction().GetId(),
	)

	return &bookingv1.CollectorAcceptBookingResponse{
		Booking: toProtoBooking(bookingFields{
			ID: row.ID, CustomerID: row.CustomerID, CollectorID: row.CollectorID,
			Status: row.Status, Address: row.Address,
			Longitude: row.Longitude, Latitude: row.Latitude,
			EstimatedKg: row.EstimatedKg, MaterialType: row.MaterialType,
			Note: row.Note, ScheduledAt: row.ScheduledAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			StationID: row.StationID, PinCode: nil, PinExpiredAt: row.PinExpiredAt,
		}),
		PointTxId: deductResp.GetTransaction().GetId(),
	}, nil
}

// ============================================================
// DriverCompleteBooking — tài xế chốt tại nhà khách.
//
// Validate PIN + TTL → đổi status DELIVERED_TO_STATION → gọi
// Point.IssuePendingReward 2 lần (User reward = kg*10, Driver = 20 EP).
// Lưu tx_id vào booking row để CollectorVerifyBooking dùng sau.
//
// Saga compensation: nếu 1 trong 2 IssuePendingReward fail thì rollback
// booking về ACCEPTED + cancel pending tx đã tạo.
// ============================================================
func (s *BookingServer) DriverCompleteBooking(ctx context.Context, req *bookingv1.DriverCompleteBookingRequest) (*bookingv1.DriverCompleteBookingResponse, error) {
	bookingID, err := uuid.Parse(req.GetBookingId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid booking_id: %v", err)
	}
	driverID, err := uuid.Parse(req.GetDriverId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid driver_id: %v", err)
	}
	pin := strings.TrimSpace(req.GetPinCode())
	if len(pin) != 4 {
		return nil, status.Error(codes.InvalidArgument, "pin_code must be 4 digits")
	}
	if req.GetDriverWeight() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "driver_weight must be positive")
	}
	proofURL := strings.TrimSpace(req.GetProofImageUrl())
	if proofURL == "" {
		return nil, status.Error(codes.InvalidArgument, "proof_image_url is required")
	}

	driverWeightKg := decimal.NewFromFloat(req.GetDriverWeight())

	// ─── 1. Atomic PIN + TTL check → đổi status ──────────────
	row, err := s.q.DriverCompleteBooking(ctx, bookingdb.DriverCompleteBookingParams{
		ID:             toPgUUID(bookingID),
		DriverWeight:   toPgNumeric(driverWeightKg),
		ProofImageUrl:  ptrString(proofURL),
		CollectorID:    toPgUUID(driverID),
		PinCode:        ptrString(pin),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.InvalidArgument, "pin invalid, expired, or booking not in accepted state")
		}
		return nil, status.Errorf(codes.Internal, "complete booking: %v", err)
	}

	customerID := fromPgUUID(row.CustomerID)
	userAmount := int64(req.GetDriverWeight()*float64(userRewardPerKg) + 0.5) // round
	idemUser := fmt.Sprintf("driver-complete-user-%s", bookingID.String())
	idemDriver := fmt.Sprintf("driver-complete-driver-%s", bookingID.String())

	// ─── 2a. IssuePendingReward cho USER ────────────────────
	userTx, err := s.pointClient.IssuePendingReward(ctx, &pointv1.IssueRewardRequest{
		UserId:         customerID,
		Amount:         userAmount,
		Source:         "booking_reward_user",
		ReferenceId:    bookingID.String(),
		IdempotencyKey: idemUser,
	})
	if err != nil {
		s.rollbackDriverComplete(ctx, bookingID, "", "", err.Error())
		return nil, status.Errorf(codes.Internal, "issue user reward: %v", err)
	}

	// ─── 2b. IssuePendingReward cho DRIVER ──────────────────
	driverTx, err := s.pointClient.IssuePendingReward(ctx, &pointv1.IssueRewardRequest{
		UserId:         driverID.String(),
		Amount:         driverRewardFlat,
		Source:         "booking_reward_driver",
		ReferenceId:    bookingID.String(),
		IdempotencyKey: idemDriver,
	})
	if err != nil {
		// Compensation: cancel user tx + rollback booking
		s.rollbackDriverComplete(ctx, bookingID, userTx.GetTransaction().GetId(), "", err.Error())
		return nil, status.Errorf(codes.Internal, "issue driver reward: %v", err)
	}

	// ─── 3. Persist tx_ids ──────────────────────────────────
	userTxUUID, _ := uuid.Parse(userTx.GetTransaction().GetId())
	driverTxUUID, _ := uuid.Parse(driverTx.GetTransaction().GetId())
	if err := s.q.SetBookingPendingTxIds(ctx, bookingdb.SetBookingPendingTxIdsParams{
		ID:                toPgUUID(bookingID),
		UserPendingTxID:   toPgUUID(userTxUUID),
		DriverPendingTxID: toPgUUID(driverTxUUID),
	}); err != nil {
		s.log.Error("SetBookingPendingTxIds failed — pending tx orphaned",
			"booking_id", bookingID,
			"user_tx", userTx.GetTransaction().GetId(),
			"driver_tx", driverTx.GetTransaction().GetId(),
			"err", err.Error(),
		)
		// Không rollback — pending tx đã settle, kệ; job reconcile sẽ link sau.
	}

	s.log.Info("DriverCompleteBooking ok",
		"booking_id", bookingID, "driver_id", driverID,
		"driver_weight_kg", req.GetDriverWeight(),
		"user_amount", userAmount, "driver_amount", driverRewardFlat,
	)

	return &bookingv1.DriverCompleteBookingResponse{
		Booking: toProtoBooking(bookingFields{
			ID: row.ID, CustomerID: row.CustomerID, CollectorID: row.CollectorID,
			Status: row.Status, Address: row.Address,
			Longitude: row.Longitude, Latitude: row.Latitude,
			EstimatedKg: row.EstimatedKg, MaterialType: row.MaterialType,
			Note: row.Note, ScheduledAt: row.ScheduledAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			StationID: row.StationID, PinCode: nil, PinExpiredAt: row.PinExpiredAt,
			DriverWeight:    row.DriverWeight,
			CollectorWeight: row.CollectorWeight,
			ProofImageURL:   row.ProofImageUrl,
		}),
		UserPointTxId:   userTx.GetTransaction().GetId(),
		DriverPointTxId: driverTx.GetTransaction().GetId(),
	}, nil
}

func (s *BookingServer) rollbackDriverComplete(ctx context.Context, bookingID uuid.UUID, userTxID, driverTxID, cause string) {
	compCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if userTxID != "" {
		if _, err := s.pointClient.CancelReward(compCtx, &pointv1.CancelRewardRequest{
			TransactionId: userTxID, Reason: "compensation_driver_complete_fail",
		}); err != nil {
			s.log.Error("compensation cancel user tx failed", "tx_id", userTxID, "err", err.Error())
		}
	}
	if driverTxID != "" {
		if _, err := s.pointClient.CancelReward(compCtx, &pointv1.CancelRewardRequest{
			TransactionId: driverTxID, Reason: "compensation_driver_complete_fail",
		}); err != nil {
			s.log.Error("compensation cancel driver tx failed", "tx_id", driverTxID, "err", err.Error())
		}
	}

	if err := s.q.RollbackDriverComplete(compCtx, toPgUUID(bookingID)); err != nil {
		s.log.Error("compensation rollback booking failed",
			"booking_id", bookingID, "cause", cause, "err", err.Error(),
		)
	} else {
		s.log.Warn("DriverCompleteBooking compensated", "booking_id", bookingID, "cause", cause)
	}
}

// ============================================================
// CollectorVerifyBooking — Vựa cân lại, thực thi Quyền Phủ Quyết.
//
//	|collector_weight − driver_weight| / driver_weight ≤ 10%
//	  → RECONCILED + ConfirmReward 2 tx pending.
//	  > 10%
//	  → CANCELLED + CancelReward 2 tx pending + DeductTrustScore(20)
//	    cho cả User lẫn Driver.
// ============================================================
func (s *BookingServer) CollectorVerifyBooking(ctx context.Context, req *bookingv1.CollectorVerifyBookingRequest) (*bookingv1.CollectorVerifyBookingResponse, error) {
	bookingID, err := uuid.Parse(req.GetBookingId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid booking_id: %v", err)
	}
	if req.GetCollectorWeight() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "collector_weight must be positive")
	}

	// Pre-check: lấy booking để biết driver_weight + tx_ids.
	pre, err := s.q.GetBookingByID(ctx, toPgUUID(bookingID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "booking not found")
		}
		return nil, status.Errorf(codes.Internal, "lookup booking: %v", err)
	}
	if pre.Status != bookingdb.BookingStatusDeliveredToStation {
		return nil, status.Errorf(codes.FailedPrecondition,
			"booking status=%q is not delivered_to_station", pre.Status)
	}
	driverWeight := fromPgNumeric(pre.DriverWeight).InexactFloat64()
	if driverWeight <= 0 {
		return nil, status.Error(codes.FailedPrecondition, "driver_weight is missing")
	}

	collectorWeight := req.GetCollectorWeight()
	deviation := math.Abs(collectorWeight-driverWeight) / driverWeight
	deviationPct := deviation * 100

	userTxID := fromPgUUID(pre.UserPendingTxID)
	driverTxID := fromPgUUID(pre.DriverPendingTxID)

	if deviation <= weightToleranceMax {
		// ─── PASS: Reconciled + Confirm 2 tx ─────────────────
		row, err := s.q.CollectorVerifyReconciled(ctx, bookingdb.CollectorVerifyReconciledParams{
			ID:              toPgUUID(bookingID),
			CollectorWeight: toPgNumeric(decimal.NewFromFloat(collectorWeight)),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, status.Error(codes.FailedPrecondition, "booking no longer in delivered_to_station")
			}
			return nil, status.Errorf(codes.Internal, "reconcile failed: %v", err)
		}

		s.confirmReward(ctx, userTxID, req.GetVerifiedBy())
		s.confirmReward(ctx, driverTxID, req.GetVerifiedBy())

		s.log.Info("CollectorVerifyBooking RECONCILED",
			"booking_id", bookingID, "deviation_pct", deviationPct,
			"driver_w", driverWeight, "collector_w", collectorWeight,
		)
		return &bookingv1.CollectorVerifyBookingResponse{
			Booking: toProtoBooking(bookingFields{
				ID: row.ID, CustomerID: row.CustomerID, CollectorID: row.CollectorID,
				Status: row.Status, Address: row.Address,
				Longitude: row.Longitude, Latitude: row.Latitude,
				EstimatedKg: row.EstimatedKg, MaterialType: row.MaterialType,
				Note: row.Note, ScheduledAt: row.ScheduledAt,
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
				StationID: row.StationID, PinCode: nil, PinExpiredAt: row.PinExpiredAt,
				DriverWeight: row.DriverWeight, CollectorWeight: row.CollectorWeight,
				ProofImageURL: row.ProofImageUrl,
			}),
			Reconciled:   true,
			DeviationPct: deviationPct,
		}, nil
	}

	// ─── FAIL: Cancel + Trust Score penalty ────────────────────
	row, err := s.q.CollectorVerifyCancelled(ctx, bookingdb.CollectorVerifyCancelledParams{
		ID:              toPgUUID(bookingID),
		CollectorWeight: toPgNumeric(decimal.NewFromFloat(collectorWeight)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Error(codes.FailedPrecondition, "booking no longer in delivered_to_station")
		}
		return nil, status.Errorf(codes.Internal, "cancel failed: %v", err)
	}

	s.cancelReward(ctx, userTxID, "weight_mismatch")
	s.cancelReward(ctx, driverTxID, "weight_mismatch")

	// Trừ trust score cả USER + DRIVER (chỉ caller "thắng" UPDATE WHERE
	// status='delivered_to_station' mới chạm tới đây → idempotent natural).
	customerID := fromPgUUID(pre.CustomerID)
	driverID := fromPgUUID(pre.CollectorID)
	s.deductTrust(ctx, customerID, "weight_mismatch_customer")
	if driverID != customerID && driverID != "00000000-0000-0000-0000-000000000000" {
		s.deductTrust(ctx, driverID, "weight_mismatch_driver")
	}

	s.log.Warn("CollectorVerifyBooking CANCELLED (fraud)",
		"booking_id", bookingID, "deviation_pct", deviationPct,
		"driver_w", driverWeight, "collector_w", collectorWeight,
	)
	return &bookingv1.CollectorVerifyBookingResponse{
		Booking: toProtoBooking(bookingFields{
			ID: row.ID, CustomerID: row.CustomerID, CollectorID: row.CollectorID,
			Status: row.Status, Address: row.Address,
			Longitude: row.Longitude, Latitude: row.Latitude,
			EstimatedKg: row.EstimatedKg, MaterialType: row.MaterialType,
			Note: row.Note, ScheduledAt: row.ScheduledAt,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			StationID: row.StationID, PinCode: nil, PinExpiredAt: row.PinExpiredAt,
			DriverWeight: row.DriverWeight, CollectorWeight: row.CollectorWeight,
			ProofImageURL: row.ProofImageUrl,
		}),
		Reconciled:   false,
		DeviationPct: deviationPct,
	}, nil
}

func (s *BookingServer) confirmReward(ctx context.Context, txID, by string) {
	if txID == "" || txID == "00000000-0000-0000-0000-000000000000" {
		return
	}
	if _, err := s.pointClient.ConfirmReward(ctx, &pointv1.ConfirmRewardRequest{
		TransactionId: txID, ConfirmedBy: by,
	}); err != nil {
		s.log.Error("ConfirmReward failed", "tx_id", txID, "err", err.Error())
	}
}

func (s *BookingServer) cancelReward(ctx context.Context, txID, reason string) {
	if txID == "" || txID == "00000000-0000-0000-0000-000000000000" {
		return
	}
	if _, err := s.pointClient.CancelReward(ctx, &pointv1.CancelRewardRequest{
		TransactionId: txID, Reason: reason,
	}); err != nil {
		s.log.Error("CancelReward failed", "tx_id", txID, "err", err.Error())
	}
}

func (s *BookingServer) deductTrust(ctx context.Context, userID, reason string) {
	if userID == "" || userID == "00000000-0000-0000-0000-000000000000" {
		return
	}
	if _, err := s.userClient.DeductTrustScore(ctx, &userv1.DeductTrustScoreRequest{
		UserId: userID, Amount: trustPenalty, Reason: reason,
	}); err != nil {
		s.log.Error("DeductTrustScore failed", "user_id", userID, "err", err.Error())
	}
}

// ============================================================
// ListMyBookings — lịch sử của 1 user.
// ============================================================
func (s *BookingServer) ListMyBookings(ctx context.Context, req *bookingv1.ListMyBookingsRequest) (*bookingv1.ListBookingsResponse, error) {
	customerID, err := uuid.Parse(req.GetCustomerId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid customer_id: %v", err)
	}
	limit := req.GetLimit()
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.q.ListMyBookings(ctx, bookingdb.ListMyBookingsParams{
		CustomerID: toPgUUID(customerID),
		Limit:      limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list my failed: %v", err)
	}

	out := make([]*bookingv1.Booking, 0, len(rows))
	for _, r := range rows {
		// PIN không lộ ra cho list view — chỉ lúc CreateBooking.
		out = append(out, toProtoBooking(bookingFields{
			ID: r.ID, CustomerID: r.CustomerID, CollectorID: r.CollectorID,
			Status: r.Status, Address: r.Address,
			Longitude: r.Longitude, Latitude: r.Latitude,
			EstimatedKg: r.EstimatedKg, MaterialType: r.MaterialType,
			Note: r.Note, ScheduledAt: r.ScheduledAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			StationID: r.StationID, PinCode: nil, PinExpiredAt: r.PinExpiredAt,
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}

// ============================================================
// ListBookings — admin xem toàn bộ, có thể filter theo status.
// ============================================================
func (s *BookingServer) ListBookings(ctx context.Context, req *bookingv1.ListBookingsRequest) (*bookingv1.ListBookingsResponse, error) {
	limit := req.GetLimit()
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	if req.GetStatus() == bookingv1.BookingStatus_BOOKING_STATUS_UNSPECIFIED {
		rows, err := s.q.ListBookings(ctx, limit)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list failed: %v", err)
		}
		out := make([]*bookingv1.Booking, 0, len(rows))
		for _, r := range rows {
			out = append(out, toProtoBooking(bookingFields{
				ID: r.ID, CustomerID: r.CustomerID, CollectorID: r.CollectorID,
				Status: r.Status, Address: r.Address,
				Longitude: r.Longitude, Latitude: r.Latitude,
				EstimatedKg: r.EstimatedKg, MaterialType: r.MaterialType,
				Note: r.Note, ScheduledAt: r.ScheduledAt,
				CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				StationID: r.StationID, PinCode: nil, PinExpiredAt: r.PinExpiredAt,
			}))
		}
		return &bookingv1.ListBookingsResponse{Bookings: out}, nil
	}

	rows, err := s.q.ListBookingsByStatus(ctx, bookingdb.ListBookingsByStatusParams{
		Status: protoStatusToDB[req.GetStatus()],
		Limit:  limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list by status failed: %v", err)
	}
	out := make([]*bookingv1.Booking, 0, len(rows))
	for _, r := range rows {
		out = append(out, toProtoBooking(bookingFields{
			ID: r.ID, CustomerID: r.CustomerID, CollectorID: r.CollectorID,
			Status: r.Status, Address: r.Address,
			Longitude: r.Longitude, Latitude: r.Latitude,
			EstimatedKg: r.EstimatedKg, MaterialType: r.MaterialType,
			Note: r.Note, ScheduledAt: r.ScheduledAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			StationID: r.StationID, PinCode: nil, PinExpiredAt: r.PinExpiredAt,
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}

// ============================================================
// ListPendingNearby — KNN PostGIS.
// ============================================================
func (s *BookingServer) ListPendingNearby(ctx context.Context, req *bookingv1.ListPendingNearbyRequest) (*bookingv1.ListBookingsResponse, error) {
	limit := req.GetLimit()
	if limit <= 0 || limit > 50 {
		limit = 5
	}
	rows, err := s.q.FindNearestBookings(ctx, bookingdb.FindNearestBookingsParams{
		StMakepoint:   req.GetLongitude(),
		StMakepoint_2: req.GetLatitude(),
		Limit:         limit,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "knn failed: %v", err)
	}
	out := make([]*bookingv1.Booking, 0, len(rows))
	for _, r := range rows {
		out = append(out, toProtoBooking(bookingFields{
			ID: r.ID, CustomerID: r.CustomerID, CollectorID: r.CollectorID,
			Status: r.Status, Address: r.Address,
			Longitude: r.Longitude, Latitude: r.Latitude,
			EstimatedKg: r.EstimatedKg, MaterialType: r.MaterialType,
			Note: r.Note, ScheduledAt: r.ScheduledAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			DistanceM: r.DistanceM,
			StationID: r.StationID, PinCode: nil, PinExpiredAt: r.PinExpiredAt,
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}

// ============================================================
// Helpers
// ============================================================

// generatePIN4 — 4-digit PIN dùng crypto/rand. Trả "0042" nếu cần.
func generatePIN4() (string, error) {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	n := int(binary.BigEndian.Uint16(buf)) % 10000
	return fmt.Sprintf("%04d", n), nil
}
