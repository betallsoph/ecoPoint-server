package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	mediav1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/media/v1"

	grpcserver "github.com/ecopoint/ecopoint/services/media/internal/grpc"
	"github.com/ecopoint/ecopoint/services/media/internal/storage"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // .env (nếu có) — silent khi vắng

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "media")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	useSSL, _ := strconv.ParseBool(envOr("MINIO_USE_SSL", "false"))
	cfg := storage.Config{
		Endpoint:   envOr("MINIO_ENDPOINT", "localhost:9000"),
		AccessKey:  mustEnv("MINIO_ACCESS_KEY"),
		SecretKey:  mustEnv("MINIO_SECRET_KEY"),
		UseSSL:     useSSL,
		Bucket:     envOr("MINIO_BUCKET", "media"),
		PublicHost: envOr("MINIO_PUBLIC_HOST", "http://localhost:9000"),
	}

	client, err := storage.NewClient(ctx, cfg)
	if err != nil {
		logger.Error("minio init failed", "err", err.Error())
		os.Exit(1)
	}
	logger.Info("minio ready", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket)

	port := envOr("GRPC_PORT", "50054")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	mediav1.RegisterMediaServiceServer(srv, grpcserver.NewMediaServer(client, cfg.Bucket, cfg.PublicHost, logger))
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, draining grpc")
		srv.GracefulStop()
	}()

	logger.Info("gRPC listening", "port", port)
	if err := srv.Serve(lis); err != nil {
		logger.Error("grpc serve failed", "err", err.Error())
		os.Exit(1)
	}
	logger.Info("media-service stopped")
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		slog.Error("missing env", "key", k)
		os.Exit(1)
	}
	return v
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
