package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	ratingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/rating/v1"

	grpcserver "github.com/ecopoint/ecopoint/services/rating/internal/grpc"
	mongorepo "github.com/ecopoint/ecopoint/services/rating/internal/mongo"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // .env (nếu có) — silent khi vắng

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "rating")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ---- MongoDB ----
	mongoClient, err := mongorepo.Connect(ctx, mustEnv("MONGO_URI"))
	if err != nil {
		logger.Error("mongo connect failed", "err", err.Error())
		os.Exit(1)
	}
	defer func() {
		shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = mongoClient.Disconnect(shutCtx)
	}()

	dbName := envOr("MONGO_DATABASE", "ecopoint_rating")
	repo := mongorepo.NewReviewRepo(mongoClient.Database(dbName))
	if err := repo.EnsureIndexes(ctx); err != nil {
		logger.Error("ensure indexes failed", "err", err.Error())
		os.Exit(1)
	}
	logger.Info("mongo ready", "database", dbName, "collection", "reviews")

	// ---- gRPC ----
	port := envOr("GRPC_PORT", "50055")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	ratingv1.RegisterRatingServiceServer(srv, grpcserver.NewRatingServer(repo, logger))
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
	logger.Info("rating-service stopped")
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
