package grpcserver

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
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

	bookingdb "github.com/ecopoint/ecopoint/services/booking/db/sqlc"
)

// Phí Vận Hành mặc định khi Vựa nhận đơn (Master Doc §1).
const stationAcceptFeeEP int64 = 20

// TTL của PIN do User cấp cho Driver — Master Doc §4.1.
const pinTTL = 10 * time.Minute

type BookingServer struct {
	bookingv1.UnimplementedBookingServiceServer
	pool        *pgxpool.Pool
	q           *bookingdb.Queries
	pointClient pointv1.PointServiceClient
	log         *slog.Logger
}

func NewBookingServer(
	pool *pgxpool.Pool,
	pointClient pointv1.PointServiceClient,
	log *slog.Logger,
) *BookingServer {
	return &BookingServer{
		pool:        pool,
		q:           bookingdb.New(pool),
		pointClient: pointClient,
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
