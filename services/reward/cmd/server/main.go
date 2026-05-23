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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"
	rewardv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/reward/v1"

	"github.com/ecopoint/ecopoint/pkg/events"
	grpcserver "github.com/ecopoint/ecopoint/services/reward/internal/grpc"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // .env (nếu có) — silent khi vắng

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "reward")
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

	// ---- Point service gRPC client ----
	pointAddr := envOr("POINT_SERVICE_ADDR", "localhost:50053")
	pointConn, err := grpc.NewClient(pointAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("point client dial failed", "err", err.Error(), "addr", pointAddr)
		os.Exit(1)
	}
	defer pointConn.Close()
	pointClient := pointv1.NewPointServiceClient(pointConn)
	logger.Info("point service client ready", "addr", pointAddr)

	// ---- Kafka producer ----
	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := envOr("KAFKA_TOPIC_REWARD_EVENTS", events.TopicRewardEvents)
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
		BatchTimeout:           50 * time.Millisecond,
		WriteTimeout:           10 * time.Second,
	}
	defer func() {
		if err := writer.Close(); err != nil {
			logger.Warn("kafka writer close failed", "err", err.Error())
		}
	}()
	logger.Info("kafka producer ready", "brokers", brokers, "topic", topic)

	// ---- gRPC server ----
	port := envOr("GRPC_PORT", "50056")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	rewardv1.RegisterRewardServiceServer(srv, grpcserver.NewRewardServer(pool, pointClient, writer, logger))
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
	logger.Info("reward-service stopped")
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
