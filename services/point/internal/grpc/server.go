package grpcserver

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/common/v1"
	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"

	pointdb "github.com/ecopoint/ecopoint/services/point/db/sqlc"
	"github.com/ecopoint/ecopoint/pkg/events"
	"github.com/ecopoint/ecopoint/pkg/mq"
)

type PointServer struct {
	pointv1.UnimplementedPointServiceServer
	pool *pgxpool.Pool
	q    *pointdb.Queries
	pub  *mq.Publisher
	log  *slog.Logger
}

func NewPointServer(pool *pgxpool.Pool, pub *mq.Publisher, log *slog.Logger) *PointServer {
	return &PointServer{
		pool: pool,
		q:    pointdb.New(pool),
		pub:  pub,
		log:  log.With("component", "point-server"),
	}
}

// AddPoints — cộng điểm trong một transaction ACID, có idempotency.
// Sau khi commit thành công sẽ publish PointAdded lên exchange `point_events`.
func (s *PointServer) AddPoints(ctx context.Context, req *pointv1.AddPointsRequest) (*pointv1.AddPointsResponse, error) {
	row, balance, err := s.applyDelta(
		ctx, req.GetUserId(), req.GetAmount(), req.GetIdempotencyKey(),
		pointdb.PointTxTypeAdd, req.GetSource().String(), req.GetReferenceId(),
	)
	if err != nil {
		return nil, err
	}

	s.publishAdded(ctx, row, balance, req.GetSource().String())

	return &pointv1.AddPointsResponse{
		Transaction: toProtoTx(row),
		NewBalance:  &commonv1.Decimal{Value: balance.String()},
	}, nil
}

// publishAdded — fire-and-log: lỗi publish không rollback tx (đã commit).
// Hệ outbox/retry sẽ là bước tiếp theo nếu cần guarantee mạnh hơn.
func (s *PointServer) publishAdded(ctx context.Context, row pointdb.PointTransaction, balance decimal.Decimal, source string) {
	if s.pub == nil {
		return
	}
	evt := events.PointAdded{
		UserID:        fromPgUUID(row.UserID).String(),
		Points:        fromPgNumeric(row.Amount).String(),
		TransactionID: fromPgUUID(row.ID).String(),
		BalanceAfter:  balance.String(),
		Source:        source,
		OccurredAt:    time.Now().UTC(),
	}
	if err := s.pub.PublishJSON(ctx, events.RoutingKeyAdded, evt); err != nil {
		s.log.Error("publish point.added failed",
			"err", err.Error(),
			"user_id", evt.UserID,
			"transaction_id", evt.TransactionID,
		)
	}
}

// DeductPoints — trừ điểm trong một transaction ACID, có idempotency.
func (s *PointServer) DeductPoints(ctx context.Context, req *pointv1.DeductPointsRequest) (*pointv1.DeductPointsResponse, error) {
	row, balance, err := s.applyDelta(
		ctx, req.GetUserId(), req.GetAmount(), req.GetIdempotencyKey(),
		pointdb.PointTxTypeDeduct, req.GetReason().String(), req.GetReferenceId(),
	)
	if err != nil {
		return nil, err
	}
	return &pointv1.DeductPointsResponse{
		Transaction: toProtoTx(row),
		NewBalance:  &commonv1.Decimal{Value: balance.String()},
	}, nil
}

// applyDelta gói toàn bộ logic vào một transaction Serializable.
// Trình tự: idempotency-check → upsert wallet → SELECT … FOR UPDATE → tính số dư mới
//           → UPDATE balance → INSERT point_transactions → COMMIT.
func (s *PointServer) applyDelta(
	ctx context.Context,
	userIDStr string,
	amountPB *commonv1.Decimal,
	idemKey string,
	txType pointdb.PointTxType,
	sourceOrReason string,
	referenceID string,
) (pointdb.PointTransaction, decimal.Decimal, error) {
	var empty pointdb.PointTransaction

	if idemKey == "" {
		return empty, decimal.Zero, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return empty, decimal.Zero, status.Errorf(codes.InvalidArgument, "invalid user_id: %v", err)
	}
	amount, err := decimal.NewFromString(amountPB.GetValue())
	if err != nil || !amount.IsPositive() {
		return empty, decimal.Zero, status.Error(codes.InvalidArgument, "amount must be a positive decimal string")
	}

	// Idempotency short-circuit (ngoài transaction để rẻ).
	if existing, err := s.q.GetTxByIdempotencyKey(ctx, idemKey); err == nil {
		return existing, fromPgNumeric(existing.BalanceAfter), nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return empty, decimal.Zero, status.Errorf(codes.Internal, "idempotency lookup failed: %v", err)
	}

	var (
		row     pointdb.PointTransaction
		balance decimal.Decimal
	)

	txErr := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		pgUserID := toPgUUID(userID)

		if _, err := qtx.UpsertWallet(ctx, pgUserID); err != nil {
			return err
		}
		wallet, err := qtx.LockWallet(ctx, pgUserID)
		if err != nil {
			return err
		}

		current := fromPgNumeric(wallet.Balance)
		switch txType {
		case pointdb.PointTxTypeAdd:
			balance = current.Add(amount)
		case pointdb.PointTxTypeDeduct:
			balance = current.Sub(amount)
			if balance.IsNegative() {
				return status.Error(codes.FailedPrecondition, "insufficient balance")
			}
		}

		if _, err := qtx.UpdateBalance(ctx, pointdb.UpdateBalanceParams{
			UserID:  pgUserID,
			Balance: toPgNumeric(balance),
		}); err != nil {
			return err
		}

		row, err = qtx.InsertPointTransaction(ctx, pointdb.InsertPointTransactionParams{
			UserID:         pgUserID,
			TxType:         txType,
			Amount:         toPgNumeric(amount),
			BalanceAfter:   toPgNumeric(balance),
			Source:         toPgText(sourceOrReason),
			ReferenceID:    toPgText(referenceID),
			IdempotencyKey: idemKey,
		})
		return err
	})

	if txErr != nil {
		if _, ok := status.FromError(txErr); ok {
			return empty, decimal.Zero, txErr
		}
		return empty, decimal.Zero, status.Errorf(codes.Internal, "tx failed: %v", txErr)
	}
	return row, balance, nil
}

func toProtoTx(row pointdb.PointTransaction) *pointv1.PointTransaction {
	return &pointv1.PointTransaction{
		Id:           fromPgUUID(row.ID).String(),
		UserId:       fromPgUUID(row.UserID).String(),
		Amount:       &commonv1.Decimal{Value: fromPgNumeric(row.Amount).String()},
		BalanceAfter: &commonv1.Decimal{Value: fromPgNumeric(row.BalanceAfter).String()},
		ReferenceId:  row.ReferenceID.String,
		CreatedAt:    timestamppb.New(row.CreatedAt.Time),
	}
}
