package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"

	pointdb "github.com/ecopoint/ecopoint/services/point/db/sqlc"
)

// PointServer — V1.3 Two-Phase Ledger.
//
//	balance_available  = tiền đã settle, được redeem / chi tiêu.
//	balance_pending    = tiền chờ Vựa xác nhận, chưa khả dụng.
//
// Mọi state change xài pgx.BeginTxFunc(Serializable) + SELECT … FOR UPDATE
// trên wallet row → race-free dù 1000 request đồng thời.
type PointServer struct {
	pointv1.UnimplementedPointServiceServer

	pool   *pgxpool.Pool
	q      *pointdb.Queries
	writer *kafka.Writer
	log    *slog.Logger
}

func NewPointServer(pool *pgxpool.Pool, writer *kafka.Writer, log *slog.Logger) *PointServer {
	return &PointServer{
		pool:   pool,
		q:      pointdb.New(pool),
		writer: writer,
		log:    log.With("component", "point-server"),
	}
}

var txOpts = pgx.TxOptions{IsoLevel: pgx.Serializable}

// ============================================================
// GetBalance
// ============================================================
func (s *PointServer) GetBalance(ctx context.Context, req *pointv1.GetBalanceRequest) (*pointv1.GetBalanceResponse, error) {
	userID, err := uuid.Parse(req.GetUserId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid user_id: %v", err)
	}
	wallet, err := s.q.UpsertWallet(ctx, toPgUUID(userID))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "upsert wallet: %v", err)
	}
	return &pointv1.GetBalanceResponse{
		BalanceAvailable: wallet.BalanceAvailable,
		BalancePending:   wallet.BalancePending,
	}, nil
}

// ============================================================
// DeductFee — trừ thẳng balance_available
// ============================================================
func (s *PointServer) DeductFee(ctx context.Context, req *pointv1.DeductFeeRequest) (*pointv1.DeductFeeResponse, error) {
	userID, err := s.validateBasic(req.GetUserId(), req.GetAmount(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}

	// Idempotency short-circuit (ngoài tx để rẻ).
	if existing, lookupErr := s.q.GetTxByIdempotencyKey(ctx, req.GetIdempotencyKey()); lookupErr == nil {
		return &pointv1.DeductFeeResponse{
			Transaction:         toProtoTx(existing),
			NewBalanceAvailable: existing.BalanceAfter, // approximate; idempotent replay
		}, nil
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "idempotency lookup: %v", lookupErr)
	}

	var (
		txRow    pointdb.PointTransaction
		newAvail int64
		newPend  int64
	)
	txErr := pgx.BeginTxFunc(ctx, s.pool, txOpts, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		pgUID := toPgUUID(userID)

		// Đảm bảo wallet tồn tại trước khi lock.
		if _, err := qtx.UpsertWallet(ctx, pgUID); err != nil {
			return err
		}

		// DeductAvailable trả 0 row khi thiếu tiền.
		wallet, err := qtx.DeductAvailable(ctx, pointdb.DeductAvailableParams{
			UserID:           pgUID,
			BalanceAvailable: req.GetAmount(),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "insufficient available balance")
			}
			return err
		}
		newAvail = wallet.BalanceAvailable
		newPend = wallet.BalancePending

		row, err := qtx.InsertDeductFeeTx(ctx, pointdb.InsertDeductFeeTxParams{
			UserID:         pgUID,
			Amount:         req.GetAmount(),
			BalanceAfter:   newAvail + newPend,
			Source:         toNullString(req.GetReason()),
			ReferenceID:    toNullString(req.GetReferenceId()),
			IdempotencyKey: req.GetIdempotencyKey(),
		})
		if err != nil {
			return err
		}
		txRow = row
		return nil
	})
	if err := unwrapTxErr(txErr); err != nil {
		return nil, err
	}

	s.log.Info("DeductFee committed",
		"user_id", userID, "amount", req.GetAmount(),
		"reason", req.GetReason(), "new_available", newAvail,
	)
	return &pointv1.DeductFeeResponse{
		Transaction:         toProtoTx(txRow),
		NewBalanceAvailable: newAvail,
	}, nil
}

// ============================================================
// IssuePendingReward — cộng vào balance_pending
// ============================================================
func (s *PointServer) IssuePendingReward(ctx context.Context, req *pointv1.IssueRewardRequest) (*pointv1.IssueRewardResponse, error) {
	userID, err := s.validateBasic(req.GetUserId(), req.GetAmount(), req.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}

	if existing, lookupErr := s.q.GetTxByIdempotencyKey(ctx, req.GetIdempotencyKey()); lookupErr == nil {
		return &pointv1.IssueRewardResponse{
			Transaction:       toProtoTx(existing),
			NewBalancePending: existing.BalanceAfter,
		}, nil
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "idempotency lookup: %v", lookupErr)
	}

	var (
		txRow    pointdb.PointTransaction
		newAvail int64
		newPend  int64
	)
	txErr := pgx.BeginTxFunc(ctx, s.pool, txOpts, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		pgUID := toPgUUID(userID)

		if _, err := qtx.UpsertWallet(ctx, pgUID); err != nil {
			return err
		}
		wallet, err := qtx.AddPending(ctx, pointdb.AddPendingParams{
			UserID:         pgUID,
			BalancePending: req.GetAmount(),
		})
		if err != nil {
			return err
		}
		newAvail = wallet.BalanceAvailable
		newPend = wallet.BalancePending

		row, err := qtx.InsertPendingRewardTx(ctx, pointdb.InsertPendingRewardTxParams{
			UserID:         pgUID,
			Amount:         req.GetAmount(),
			BalanceAfter:   newAvail + newPend,
			Source:         toNullString(req.GetSource()),
			ReferenceID:    toNullString(req.GetReferenceId()),
			IdempotencyKey: req.GetIdempotencyKey(),
		})
		if err != nil {
			return err
		}
		txRow = row
		return nil
	})
	if err := unwrapTxErr(txErr); err != nil {
		return nil, err
	}

	s.log.Info("IssuePendingReward committed",
		"user_id", userID, "amount", req.GetAmount(),
		"source", req.GetSource(), "new_pending", newPend,
	)
	return &pointv1.IssueRewardResponse{
		Transaction:       toProtoTx(txRow),
		NewBalancePending: newPend,
	}, nil
}

// ============================================================
// ConfirmReward — pending → available
// ============================================================
func (s *PointServer) ConfirmReward(ctx context.Context, req *pointv1.ConfirmRewardRequest) (*pointv1.ConfirmRewardResponse, error) {
	txID, err := uuid.Parse(req.GetTransactionId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid transaction_id: %v", err)
	}

	var (
		txRow    pointdb.PointTransaction
		newAvail int64
		newPend  int64
	)
	txErr := pgx.BeginTxFunc(ctx, s.pool, txOpts, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)

		row, err := qtx.GetTxByID(ctx, toPgUUID(txID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.NotFound, "transaction not found")
			}
			return err
		}

		// Idempotent: đã confirmed → return ngay không state change.
		if row.Status == "available" {
			txRow = row
			w, werr := qtx.GetWallet(ctx, row.UserID)
			if werr == nil {
				newAvail = w.BalanceAvailable
				newPend = w.BalancePending
			}
			return nil
		}
		if row.Status != "pending" {
			return status.Errorf(codes.FailedPrecondition, "tx status=%q is not pending", row.Status)
		}

		// Lock wallet trước khi chuyển pending → available.
		if _, err := qtx.LockWallet(ctx, row.UserID); err != nil {
			return err
		}
		wallet, err := qtx.ConfirmPendingBalance(ctx, pointdb.ConfirmPendingBalanceParams{
			UserID:         row.UserID,
			BalancePending: row.Amount,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "pending balance insufficient (data inconsistency)")
			}
			return err
		}
		updated, err := qtx.MarkTxAvailable(ctx, row.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "tx no longer pending")
			}
			return err
		}
		txRow = updated
		newAvail = wallet.BalanceAvailable
		newPend = wallet.BalancePending
		return nil
	})
	if err := unwrapTxErr(txErr); err != nil {
		return nil, err
	}

	// Fire-and-log: notify user "X điểm đã vào ví". Lỗi publish không
	// rollback tx (đã commit).
	s.publishKafka(ctx, "point.confirmed", txRow)
	s.log.Info("ConfirmReward committed",
		"tx_id", req.GetTransactionId(), "confirmed_by", req.GetConfirmedBy(),
		"new_available", newAvail,
	)
	return &pointv1.ConfirmRewardResponse{
		Transaction:         toProtoTx(txRow),
		NewBalanceAvailable: newAvail,
		NewBalancePending:   newPend,
	}, nil
}

// ============================================================
// CancelReward — pending → cancelled
// ============================================================
func (s *PointServer) CancelReward(ctx context.Context, req *pointv1.CancelRewardRequest) (*pointv1.CancelRewardResponse, error) {
	txID, err := uuid.Parse(req.GetTransactionId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid transaction_id: %v", err)
	}

	var (
		txRow   pointdb.PointTransaction
		newPend int64
	)
	txErr := pgx.BeginTxFunc(ctx, s.pool, txOpts, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)

		row, err := qtx.GetTxByID(ctx, toPgUUID(txID))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.NotFound, "transaction not found")
			}
			return err
		}
		if row.Status == "cancelled" {
			txRow = row
			w, werr := qtx.GetWallet(ctx, row.UserID)
			if werr == nil {
				newPend = w.BalancePending
			}
			return nil
		}
		if row.Status != "pending" {
			return status.Errorf(codes.FailedPrecondition, "cannot cancel tx with status %q", row.Status)
		}

		if _, err := qtx.LockWallet(ctx, row.UserID); err != nil {
			return err
		}
		wallet, err := qtx.CancelPendingBalance(ctx, pointdb.CancelPendingBalanceParams{
			UserID:         row.UserID,
			BalancePending: row.Amount,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "pending balance inconsistency")
			}
			return err
		}
		updated, err := qtx.MarkTxCancelled(ctx, row.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return status.Error(codes.FailedPrecondition, "tx no longer pending")
			}
			return err
		}
		txRow = updated
		newPend = wallet.BalancePending
		return nil
	})
	if err := unwrapTxErr(txErr); err != nil {
		return nil, err
	}

	s.log.Warn("CancelReward committed",
		"tx_id", req.GetTransactionId(), "reason", req.GetReason(),
		"new_pending", newPend,
	)
	return &pointv1.CancelRewardResponse{
		Transaction:       toProtoTx(txRow),
		NewBalancePending: newPend,
	}, nil
}

// ============================================================
// Helpers
// ============================================================

func (s *PointServer) validateBasic(userIDStr string, amount int64, idemKey string) (uuid.UUID, error) {
	if idemKey == "" {
		return uuid.Nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if amount <= 0 {
		return uuid.Nil, status.Error(codes.InvalidArgument, "amount must be positive")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "invalid user_id: %v", err)
	}
	return userID, nil
}

func unwrapTxErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Errorf(codes.Internal, "tx failed: %v", err)
}

func toProtoTx(row pointdb.PointTransaction) *pointv1.PointTransaction {
	return &pointv1.PointTransaction{
		Id:             fromPgUUID(row.ID).String(),
		UserId:         fromPgUUID(row.UserID).String(),
		TxType:         string(row.TxType),
		Amount:         row.Amount,
		BalanceAfter:   row.BalanceAfter,
		Status:         row.Status,
		Source:         derefString(row.Source),
		ReferenceId:    derefString(row.ReferenceID),
		IdempotencyKey: row.IdempotencyKey,
		CreatedAt:      timestamppb.New(row.CreatedAt.Time),
	}
}

// publishKafka — fire-and-log, KHÔNG block tx response.
func (s *PointServer) publishKafka(ctx context.Context, kind string, row pointdb.PointTransaction) {
	if s.writer == nil {
		return
	}
	payload := map[string]any{
		"kind":          kind,
		"transactionId": fromPgUUID(row.ID).String(),
		"userId":        fromPgUUID(row.UserID).String(),
		"amount":        row.Amount,
		"status":        row.Status,
		"source":        derefString(row.Source),
		"occurredAt":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("marshal point event failed", "err", err.Error())
		return
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.writer.WriteMessages(pubCtx, kafka.Message{
		Key:   []byte(fromPgUUID(row.UserID).String()),
		Value: body,
	}); err != nil {
		s.log.Error("kafka publish failed",
			"err", err.Error(), "kind", kind,
			"tx_id", fromPgUUID(row.ID).String(),
		)
	}
}
