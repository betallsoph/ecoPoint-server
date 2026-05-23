package grpcserver

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
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

func toPgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// Numeric ↔ shopspring/decimal: dùng big.Int + exponent để giữ chính xác tuyệt đối.
func toPgNumeric(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{
		Int:   d.Coefficient(),
		Exp:   d.Exponent(),
		Valid: true,
	}
}

func fromPgNumeric(n pgtype.Numeric) decimal.Decimal {
	if !n.Valid || n.NaN || n.Int == nil {
		return decimal.Zero
	}
	return decimal.NewFromBigInt(n.Int, n.Exp)
}
