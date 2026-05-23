package mongorepo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Review — document trong collection `reviews`.
type Review struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	BookingID   string             `bson:"booking_id"`
	UserID      string             `bson:"user_id"`
	CollectorID string             `bson:"collector_id"`
	Rating      int32              `bson:"rating"`
	Comment     string             `bson:"comment,omitempty"`
	MediaURLs   []string           `bson:"media_urls,omitempty"`
	CreatedAt   time.Time          `bson:"created_at"`
}

// Connect mở Mongo client + ping kiểm tra; đóng client khi context kết thúc.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(dialCtx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(dialCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, nil
}

// ReviewRepo gói collection `reviews`.
type ReviewRepo struct {
	coll *mongo.Collection
}

func NewReviewRepo(db *mongo.Database) *ReviewRepo {
	return &ReviewRepo{coll: db.Collection("reviews")}
}

// EnsureIndexes nên gọi 1 lần lúc khởi động.
func (r *ReviewRepo) EnsureIndexes(ctx context.Context) error {
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "booking_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_booking_id"),
		},
		{
			Keys:    bson.D{{Key: "collector_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("collector_recent"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("user_recent"),
		},
	})
	return err
}

// Insert chèn review mới; trả lỗi nếu booking_id đã có review (unique index).
func (r *ReviewRepo) Insert(ctx context.Context, rv *Review) error {
	if rv.CreatedAt.IsZero() {
		rv.CreatedAt = time.Now().UTC()
	}
	res, err := r.coll.InsertOne(ctx, rv)
	if err != nil {
		return err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		rv.ID = oid
	}
	return nil
}
