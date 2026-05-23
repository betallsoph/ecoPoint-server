package grpcserver

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/common/v1"
	rewardv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/reward/v1"

	rewarddb "github.com/ecopoint/ecopoint/services/reward/db/sqlc"
)

func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

func fromPgUUID(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return uuid.UUID(p.Bytes)
}

func toPgNumeric(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{Int: d.Coefficient(), Exp: d.Exponent(), Valid: true}
}

func fromPgNumeric(n pgtype.Numeric) decimal.Decimal {
	if !n.Valid || n.NaN || n.Int == nil {
		return decimal.Zero
	}
	return decimal.NewFromBigInt(n.Int, n.Exp)
}

func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var statusDBToProto = map[rewarddb.RedemptionStatus]rewardv1.RedemptionStatus{
	rewarddb.RedemptionStatusPending:   rewardv1.RedemptionStatus_REDEMPTION_STATUS_PENDING,
	rewarddb.RedemptionStatusCompleted: rewardv1.RedemptionStatus_REDEMPTION_STATUS_COMPLETED,
	rewarddb.RedemptionStatusCancelled: rewardv1.RedemptionStatus_REDEMPTION_STATUS_CANCELLED,
}

func toProtoRedemption(r rewarddb.Redemption) *rewardv1.Redemption {
	p := &rewardv1.Redemption{
		Id:            fromPgUUID(r.ID).String(),
		VoucherId:     fromPgUUID(r.VoucherID).String(),
		UserId:        fromPgUUID(r.UserID).String(),
		PointCost:     &commonv1.Decimal{Value: fromPgNumeric(r.PointCost).String()},
		Status:        statusDBToProto[r.Status],
		PointTxId:     derefString(r.PointTxID),
		FailureReason: derefString(r.FailureReason),
		CreatedAt:     timestamppb.New(r.CreatedAt.Time),
	}
	if r.CompletedAt.Valid {
		p.CompletedAt = timestamppb.New(r.CompletedAt.Time)
	}
	return p
}
