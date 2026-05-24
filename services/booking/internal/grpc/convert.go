package grpcserver

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/common/v1"
	bookingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/booking/v1"

	bookingdb "github.com/ecopoint/ecopoint/services/booking/db/sqlc"
)

func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

func fromPgUUID(p pgtype.UUID) string {
	if !p.Valid {
		return ""
	}
	return uuid.UUID(p.Bytes).String()
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

func toPgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
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

// ----- Status mapping -----
var dbStatusToProto = map[bookingdb.BookingStatus]bookingv1.BookingStatus{
	bookingdb.BookingStatusPending:             bookingv1.BookingStatus_BOOKING_STATUS_PENDING,
	bookingdb.BookingStatusAccepted:            bookingv1.BookingStatus_BOOKING_STATUS_ACCEPTED,
	bookingdb.BookingStatusCollecting:          bookingv1.BookingStatus_BOOKING_STATUS_COLLECTING,
	bookingdb.BookingStatusCompleted:           bookingv1.BookingStatus_BOOKING_STATUS_COMPLETED,
	bookingdb.BookingStatusCancelled:           bookingv1.BookingStatus_BOOKING_STATUS_CANCELLED,
	bookingdb.BookingStatusDeliveredToStation:  bookingv1.BookingStatus_BOOKING_STATUS_DELIVERED_TO_STATION,
	bookingdb.BookingStatusReconciled:          bookingv1.BookingStatus_BOOKING_STATUS_RECONCILED,
}

var protoStatusToDB = map[bookingv1.BookingStatus]bookingdb.BookingStatus{
	bookingv1.BookingStatus_BOOKING_STATUS_PENDING:              bookingdb.BookingStatusPending,
	bookingv1.BookingStatus_BOOKING_STATUS_ACCEPTED:             bookingdb.BookingStatusAccepted,
	bookingv1.BookingStatus_BOOKING_STATUS_COLLECTING:           bookingdb.BookingStatusCollecting,
	bookingv1.BookingStatus_BOOKING_STATUS_COMPLETED:            bookingdb.BookingStatusCompleted,
	bookingv1.BookingStatus_BOOKING_STATUS_CANCELLED:            bookingdb.BookingStatusCancelled,
	bookingv1.BookingStatus_BOOKING_STATUS_DELIVERED_TO_STATION: bookingdb.BookingStatusDeliveredToStation,
	bookingv1.BookingStatus_BOOKING_STATUS_RECONCILED:           bookingdb.BookingStatusReconciled,
}

// ----- Material mapping -----
var dbMaterialToProto = map[bookingdb.MaterialType]bookingv1.MaterialType{
	bookingdb.MaterialTypePaper:   bookingv1.MaterialType_MATERIAL_TYPE_PAPER,
	bookingdb.MaterialTypePlastic: bookingv1.MaterialType_MATERIAL_TYPE_PLASTIC,
	bookingdb.MaterialTypeMetal:   bookingv1.MaterialType_MATERIAL_TYPE_METAL,
	bookingdb.MaterialTypeMixed:   bookingv1.MaterialType_MATERIAL_TYPE_MIXED,
}

var protoMaterialToDB = map[bookingv1.MaterialType]bookingdb.MaterialType{
	bookingv1.MaterialType_MATERIAL_TYPE_PAPER:   bookingdb.MaterialTypePaper,
	bookingv1.MaterialType_MATERIAL_TYPE_PLASTIC: bookingdb.MaterialTypePlastic,
	bookingv1.MaterialType_MATERIAL_TYPE_METAL:   bookingdb.MaterialTypeMetal,
	bookingv1.MaterialType_MATERIAL_TYPE_MIXED:   bookingdb.MaterialTypeMixed,
}

// pgTimestampOrNil → *timestamppb.Timestamp (nil khi DB NULL).
func pgTimestampPB(t pgtype.Timestamptz) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}

// Build proto Booking từ một CreateBookingRow / ListMyBookingsRow / FindNearestBookingsRow.
// Vì các Row struct giống nhau về cấu trúc (chỉ thêm distance_m ở FindNearestBookings),
// dùng generic-like via accessor function.
type bookingFields struct {
	ID           pgtype.UUID
	CustomerID   pgtype.UUID
	CollectorID  pgtype.UUID
	Status       bookingdb.BookingStatus
	Address      string
	Longitude    float64
	Latitude     float64
	EstimatedKg  pgtype.Numeric
	MaterialType bookingdb.MaterialType
	Note         *string
	ScheduledAt  pgtype.Timestamptz
	CreatedAt    pgtype.Timestamptz
	UpdatedAt    pgtype.Timestamptz
	DistanceM    float64 // 0 nếu không phải nearest
	// V1.3 fields
	StationID    pgtype.UUID
	PinCode      *string
	PinExpiredAt pgtype.Timestamptz
}

func toProtoBooking(b bookingFields) *bookingv1.Booking {
	return &bookingv1.Booking{
		Id:           fromPgUUID(b.ID),
		CustomerId:   fromPgUUID(b.CustomerID),
		CollectorId:  fromPgUUID(b.CollectorID),
		Status:       dbStatusToProto[b.Status],
		Address:      b.Address,
		Longitude:    b.Longitude,
		Latitude:     b.Latitude,
		EstimatedKg:  &commonv1.Decimal{Value: fromPgNumeric(b.EstimatedKg).String()},
		MaterialType: dbMaterialToProto[b.MaterialType],
		Note:         derefString(b.Note),
		ScheduledAt:  pgTimestampPB(b.ScheduledAt),
		CreatedAt:    pgTimestampPB(b.CreatedAt),
		UpdatedAt:    pgTimestampPB(b.UpdatedAt),
		DistanceM:    b.DistanceM,
		StationId:    fromPgUUID(b.StationID),
		PinCode:      derefString(b.PinCode),
		PinExpiredAt: pgTimestampPB(b.PinExpiredAt),
	}
}
