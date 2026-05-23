package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"

	grpcserver "github.com/ecopoint/ecopoint/services/point/internal/grpc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "point")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ---- Postgres ----
	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		logger.Error("pgxpool init failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	// ---- Kafka producer ----
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := envOr("KAFKA_TOPIC_POINT_EVENTS", "point-events")
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{}, // partition theo key=UserID → giữ thứ tự cho cùng 1 user
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
		BatchTimeout:           50 * time.Millisecond,
		WriteTimeout:           10 * time.Second,
		Async:                  false,
	}
	defer func() {
		if err := writer.Close(); err != nil {
			logger.Warn("kafka writer close failed", "err", err.Error())
		}
	}()
	logger.Info("kafka producer ready", "brokers", brokers, "topic", topic)

	// ---- gRPC ----
	port := envOr("GRPC_PORT", "50053")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	pointv1.RegisterPointServiceServer(srv, grpcserver.NewPointServer(pool, writer, logger))
	reflection.Register(srv)

	// Graceful shutdown: chờ tín hiệu → drain RPC → đóng writer.
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
	logger.Info("point-service stopped")
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
