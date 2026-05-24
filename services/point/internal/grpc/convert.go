package grpcserver

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// V1.3: bỏ shopspring/decimal. Mọi cột tiền là BIGINT → int64 native.

func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

func fromPgUUID(p pgtype.UUID) uuid.UUID {
	if !p.Valid {
		return uuid.Nil
	}
	return uuid.UUID(p.Bytes)
}

// sqlc gen `emit_pointers_for_null_types: true` → cột TEXT nullable = *string.
func toNullString(s string) *string {
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
