package grpcserver

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	bookingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/booking/v1"

	bookingdb "github.com/ecopoint/ecopoint/services/booking/db/sqlc"
)

type BookingServer struct {
	bookingv1.UnimplementedBookingServiceServer
	pool *pgxpool.Pool
	q    *bookingdb.Queries
	log  *slog.Logger
}

func NewBookingServer(pool *pgxpool.Pool, log *slog.Logger) *BookingServer {
	return &BookingServer{
		pool: pool,
		q:    bookingdb.New(pool),
		log:  log.With("component", "booking-server"),
	}
}

// CreateBooking — user đặt đơn thu gom, có toạ độ GPS.
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

	row, err := s.q.CreateBooking(ctx, bookingdb.CreateBookingParams{
		CustomerID:    toPgUUID(customerID),
		Address:       address,
		StMakepoint:   req.GetLongitude(),
		StMakepoint_2: req.GetLatitude(),
		EstimatedKg:   toPgNumeric(estimatedKg),
		MaterialType:  material,
		Note:          ptrString(strings.TrimSpace(req.GetNote())),
		ScheduledAt:   scheduledAt,
	})
	if err != nil {
		s.log.Error("create booking failed", "err", err.Error())
		return nil, status.Errorf(codes.Internal, "create failed: %v", err)
	}

	s.log.Info("booking created",
		"booking_id", fromPgUUID(row.ID),
		"customer_id", req.GetCustomerId(),
		"address", address,
	)

	return &bookingv1.CreateBookingResponse{
		Booking: toProtoBooking(bookingFields{
			ID:           row.ID,
			CustomerID:   row.CustomerID,
			CollectorID:  row.CollectorID,
			Status:       row.Status,
			Address:      row.Address,
			Longitude:    row.Longitude,
			Latitude:     row.Latitude,
			EstimatedKg:  row.EstimatedKg,
			MaterialType: row.MaterialType,
			Note:         row.Note,
			ScheduledAt:  row.ScheduledAt,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		}),
	}, nil
}

// ListMyBookings — lịch sử đơn của 1 user.
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
		out = append(out, toProtoBooking(bookingFields{
			ID: r.ID, CustomerID: r.CustomerID, CollectorID: r.CollectorID,
			Status: r.Status, Address: r.Address,
			Longitude: r.Longitude, Latitude: r.Latitude,
			EstimatedKg: r.EstimatedKg, MaterialType: r.MaterialType,
			Note: r.Note, ScheduledAt: r.ScheduledAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}

// ListBookings — admin xem toàn bộ, có thể filter theo status.
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
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}

// ListPendingNearby — KNN: 5 booking pending gần toạ độ truyền vào.
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
		}))
	}
	return &bookingv1.ListBookingsResponse{Bookings: out}, nil
}
