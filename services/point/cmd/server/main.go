package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"

	"github.com/ecopoint/ecopoint/pkg/events"
	"github.com/ecopoint/ecopoint/pkg/mq"
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

	// ---- RabbitMQ (auto-reconnect) ----
	amqpConn, err := mq.Dial(ctx, envOr("RABBITMQ_URL", "amqp://ecopoint:ecopoint_secret@localhost:5672/"), logger)
	if err != nil {
		logger.Error("rabbitmq dial failed", "err", err.Error())
		os.Exit(1)
	}
	defer amqpConn.Close()

	publisher, err := mq.NewPublisher(amqpConn, events.ExchangePointEvents, logger)
	if err != nil {
		logger.Error("publisher init failed", "err", err.Error())
		os.Exit(1)
	}
	defer publisher.Close()

	// ---- gRPC ----
	port := envOr("GRPC_PORT", "50053")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	pointv1.RegisterPointServiceServer(srv, grpcserver.NewPointServer(pool, publisher, logger))
	reflection.Register(srv)

	// Graceful shutdown: chờ tín hiệu → drain RPC → đóng AMQP → kết thúc.
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
