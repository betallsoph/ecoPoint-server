package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/common/v1"
	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"
	rewardv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/reward/v1"

	"github.com/ecopoint/ecopoint/pkg/events"
	rewarddb "github.com/ecopoint/ecopoint/services/reward/db/sqlc"
)

type RewardServer struct {
	rewardv1.UnimplementedRewardServiceServer

	pool        *pgxpool.Pool
	q           *rewarddb.Queries
	pointClient pointv1.PointServiceClient
	writer      *kafka.Writer
	log         *slog.Logger
}

func NewRewardServer(
	pool *pgxpool.Pool,
	pointClient pointv1.PointServiceClient,
	writer *kafka.Writer,
	log *slog.Logger,
) *RewardServer {
	return &RewardServer{
		pool:        pool,
		q:           rewarddb.New(pool),
		pointClient: pointClient,
		writer:      writer,
		log:         log.With("component", "reward-server"),
	}
}

// RedeemVoucher — Orchestration của Saga đổi voucher.
//
// Trình tự:
//  1. Tx1 (local): atomic decrement stock + INSERT redemption(status='pending').
//  2. RPC: gọi PointService.DeductPoints với idempotency_key = "redeem-<redemption_id>".
//  3. Thành công → Tx2: UPDATE redemption(status='completed') + publish event lên Kafka.
//     Thất bại → COMPENSATE Tx2': UPDATE stock += 1 + UPDATE redemption(status='cancelled').
//
// Lưu ý ACID/Saga:
//   - Stock decrement là atomic SQL UPDATE (không cần FOR UPDATE) — concurrent
//     request cùng voucher sẽ tự serialize ở row-level write lock.
//   - Compensation chạy với context.WithoutCancel để không bị hủy giữa chừng nếu
//     client RPC đã ngắt.
//   - Mọi RPC ra Point Service đều mang idempotency_key bền vững (derive từ
//     redemption_id) — Saga retry an toàn, không trừ điểm 2 lần.
func (s *RewardServer) RedeemVoucher(ctx context.Context, req *rewardv1.RedeemVoucherRequest) (*rewardv1.RedeemVoucherResponse, error) {
	// ---------- Validate ----------
	idemKey := req.GetIdempotencyKey()
	if idemKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key required")
	}
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid user_id: %v", err)
	}
	voucherID, err := uuid.Parse(req.GetVoucherId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid voucher_id: %v", err)
	}

	// ---------- Idempotency short-circuit ----------
	if existing, err := s.q.GetRedemptionByIdempotencyKey(ctx, idemKey); err == nil {
		return &rewardv1.RedeemVoucherResponse{
			Redemption: toProtoRedemption(existing),
			NewBalance: &commonv1.Decimal{Value: "0"}, // client lấy số dư qua user/point service nếu cần
		}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "idempotency lookup: %v", err)
	}

	// =================== SAGA STEP 1: reserve stock + pending log ===================
	var (
		redemption rewarddb.Redemption
		pointCost  decimal.Decimal
	)
	err = pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)

		voucher, err := qtx.ReserveVoucherStock(ctx, toPgUUID(voucherID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "voucher out of stock or inactive")
			}
			return fmt.Errorf("reserve stock: %w", err)
		}
		pointCost = fromPgNumeric(voucher.PointCost)

		redemption, err = qtx.InsertPendingRedemption(ctx, rewarddb.InsertPendingRedemptionParams{
			VoucherID:      voucher.ID,
			UserID:         toPgUUID(userID),
			PointCost:      voucher.PointCost,
			IdempotencyKey: idemKey,
		})
		return err
	})
	if err != nil {
		if _, ok := status.FromError(err); ok {
			return nil, err
		}
		return nil, status.Errorf(codes.Internal, "saga step1 failed: %v", err)
	}

	redemptionUUID := fromPgUUID(redemption.ID)
	s.log.Info("saga: stock reserved",
		"redemption_id", redemptionUUID,
		"user_id", userID,
		"voucher_id", voucherID,
		"point_cost", pointCost.String(),
	)

	// =================== SAGA STEP 2: call Point Service ===================
	deductCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	deductResp, deductErr := s.pointClient.DeductPoints(deductCtx, &pointv1.DeductPointsRequest{
		UserId:         userID.String(),
		Amount:         &commonv1.Decimal{Value: pointCost.String()},
		Reason:         pointv1.PointDeductReason_POINT_DEDUCT_REASON_REDEEM,
		ReferenceId:    redemptionUUID.String(),
		IdempotencyKey: "redeem-" + redemptionUUID.String(),
	})

	if deductErr != nil {
		// -------- COMPENSATION: rollback stock + đánh dấu redemption cancelled --------
		compCtx, compCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer compCancel()

		if compErr := s.compensate(compCtx, redemption.ID, voucherID, deductErr); compErr != nil {
			// Compensation thất bại → cần job reconcile xử lý. Ghi log lớn tiếng để alert.
			s.log.Error("compensation FAILED — manual reconcile required",
				"redemption_id", redemptionUUID,
				"voucher_id", voucherID,
				"cause", deductErr.Error(),
				"comp_err", compErr.Error(),
			)
		} else {
			s.log.Warn("saga: compensated",
				"redemption_id", redemptionUUID,
				"cause", deductErr.Error(),
			)
		}

		// Lan truyền lỗi gốc dạng gRPC status nếu có.
		if st, ok := status.FromError(deductErr); ok {
			return nil, status.Errorf(st.Code(), "deduct points failed: %s", st.Message())
		}
		return nil, status.Errorf(codes.Internal, "deduct points failed: %v", deductErr)
	}

	// =================== SAGA STEP 3: complete + publish ===================
	pointTxID := deductResp.GetTransaction().GetId()
	completed, err := s.q.CompleteRedemption(ctx, rewarddb.CompleteRedemptionParams{
		ID:        redemption.ID,
		PointTxID: ptrString(pointTxID),
	})
	if err != nil {
		// Điểm đã trừ nhưng update redemption thất bại → để reconcile job xử lý sau.
		s.log.Error("complete redemption failed — points already deducted",
			"redemption_id", redemptionUUID,
			"point_tx_id", pointTxID,
			"err", err.Error(),
		)
		return nil, status.Errorf(codes.Internal, "complete failed: %v", err)
	}

	s.publishRedeemed(ctx, completed, pointTxID)

	newBalance := decimal.Zero
	if v := deductResp.GetNewBalance().GetValue(); v != "" {
		newBalance, _ = decimal.NewFromString(v)
	}

	s.log.Info("saga: redemption completed",
		"redemption_id", redemptionUUID,
		"user_id", userID,
		"voucher_id", voucherID,
		"point_tx_id", pointTxID,
		"new_balance", newBalance.String(),
	)

	return &rewardv1.RedeemVoucherResponse{
		Redemption: toProtoRedemption(completed),
		NewBalance: &commonv1.Decimal{Value: newBalance.String()},
	}, nil
}

func (s *RewardServer) compensate(ctx context.Context, redemptionID pgtype.UUID, voucherID uuid.UUID, cause error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if _, err := qtx.ReleaseVoucherStock(ctx, toPgUUID(voucherID)); err != nil {
			return fmt.Errorf("release stock: %w", err)
		}
		_, err := qtx.CancelRedemption(ctx, rewarddb.CancelRedemptionParams{
			ID:            redemptionID,
			FailureReason: ptrString(cause.Error()),
		})
		return err
	})
}

func (s *RewardServer) publishRedeemed(ctx context.Context, r rewarddb.Redemption, pointTxID string) {
	if s.writer == nil {
		return
	}
	evt := events.RewardRedeemed{
		RedemptionID: fromPgUUID(r.ID).String(),
		UserID:       fromPgUUID(r.UserID).String(),
		VoucherID:    fromPgUUID(r.VoucherID).String(),
		PointCost:    fromPgNumeric(r.PointCost).String(),
		PointTxID:    pointTxID,
		OccurredAt:   time.Now().UTC(),
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		s.log.Error("marshal reward.redeemed failed", "err", err.Error())
		return
	}

	// Tách khỏi RPC ctx: client có thể đã ngắt, nhưng Kafka write vẫn nên flush.
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.writer.WriteMessages(pubCtx, kafka.Message{
		Key:   []byte(evt.UserID),
		Value: payload,
		Time:  evt.OccurredAt,
	}); err != nil {
		s.log.Error("kafka publish reward.redeemed failed",
			"err", err.Error(),
			"topic", s.writer.Topic,
			"redemption_id", evt.RedemptionID,
		)
	}
}
