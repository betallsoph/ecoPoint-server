package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	mediav1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/media/v1"
)

const presignTTL = 15 * time.Minute

type MediaServer struct {
	mediav1.UnimplementedMediaServiceServer
	client        *minio.Client
	defaultBucket string
	publicHost    string
	log           *slog.Logger
}

func NewMediaServer(client *minio.Client, defaultBucket, publicHost string, log *slog.Logger) *MediaServer {
	return &MediaServer{
		client:        client,
		defaultBucket: defaultBucket,
		publicHost:    strings.TrimRight(publicHost, "/"),
		log:           log.With("component", "media-server"),
	}
}

func (s *MediaServer) GeneratePresignedUrl(ctx context.Context, req *mediav1.GeneratePresignedUrlRequest) (*mediav1.GeneratePresignedUrlResponse, error) {
	filename := strings.TrimSpace(req.GetFilename())
	if filename == "" {
		return nil, status.Error(codes.InvalidArgument, "filename is required")
	}

	bucket := req.GetBucket()
	if bucket == "" {
		bucket = s.defaultBucket
	}

	// Đường dẫn object: <yyyy/mm/dd>/<uuid>-<safe-filename>
	// → tránh collision + dễ phân vùng theo ngày khi backup / lifecycle.
	objectKey := fmt.Sprintf("%s/%s-%s",
		time.Now().UTC().Format("2006/01/02"),
		uuid.NewString(),
		sanitize(path.Base(filename)),
	)

	uploadURL, err := s.client.PresignedPutObject(ctx, bucket, objectKey, presignTTL)
	if err != nil {
		s.log.Error("presign failed", "err", err.Error(), "bucket", bucket, "object", objectKey)
		return nil, status.Errorf(codes.Internal, "presign failed: %v", err)
	}

	return &mediav1.GeneratePresignedUrlResponse{
		UploadUrl: uploadURL.String(),
		ObjectKey: objectKey,
		PublicUrl: fmt.Sprintf("%s/%s/%s", s.publicHost, bucket, objectKey),
		ExpiresAt: timestamppb.New(time.Now().Add(presignTTL)),
	}, nil
}

// sanitize giữ ký tự an toàn cho object key; thay phần còn lại bằng '-'.
func sanitize(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		out = "file"
	}
	return out
}
