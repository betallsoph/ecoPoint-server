package grpcserver

import (
	"context"
	"log/slog"
	"strings"

	"go.mongodb.org/mongo-driver/mongo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	ratingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/rating/v1"

	mongorepo "github.com/ecopoint/ecopoint/services/rating/internal/mongo"
)

type RatingServer struct {
	ratingv1.UnimplementedRatingServiceServer
	repo *mongorepo.ReviewRepo
	log  *slog.Logger
}

func NewRatingServer(repo *mongorepo.ReviewRepo, log *slog.Logger) *RatingServer {
	return &RatingServer{
		repo: repo,
		log:  log.With("component", "rating-server"),
	}
}

func (s *RatingServer) SubmitReview(ctx context.Context, req *ratingv1.SubmitReviewRequest) (*ratingv1.SubmitReviewResponse, error) {
	bookingID := strings.TrimSpace(req.GetBookingId())
	userID := strings.TrimSpace(req.GetUserId())
	collectorID := strings.TrimSpace(req.GetCollectorId())

	if bookingID == "" || userID == "" || collectorID == "" {
		return nil, status.Error(codes.InvalidArgument, "booking_id, user_id, collector_id are required")
	}
	if req.GetRating() < 1 || req.GetRating() > 5 {
		return nil, status.Error(codes.InvalidArgument, "rating must be between 1 and 5")
	}

	rv := &mongorepo.Review{
		BookingID:   bookingID,
		UserID:      userID,
		CollectorID: collectorID,
		Rating:      req.GetRating(),
		Comment:     req.GetComment(),
		MediaURLs:   req.GetMediaUrls(),
	}

	if err := s.repo.Insert(ctx, rv); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, status.Error(codes.AlreadyExists, "review already exists for this booking")
		}
		s.log.Error("insert review failed", "err", err.Error(), "booking_id", bookingID)
		return nil, status.Errorf(codes.Internal, "insert failed: %v", err)
	}

	return &ratingv1.SubmitReviewResponse{
		Review: &ratingv1.Review{
			Id:          rv.ID.Hex(),
			BookingId:   rv.BookingID,
			UserId:      rv.UserID,
			CollectorId: rv.CollectorID,
			Rating:      rv.Rating,
			Comment:     rv.Comment,
			MediaUrls:   rv.MediaURLs,
			CreatedAt:   timestamppb.New(rv.CreatedAt),
		},
	}, nil
}
