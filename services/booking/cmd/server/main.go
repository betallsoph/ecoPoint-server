package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	bookingv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/booking/v1"
	pointv1 "github.com/ecopoint/ecopoint/shared/libs/go/ecopoint/point/v1"

	grpcserver "github.com/ecopoint/ecopoint/services/booking/internal/grpc"
)

func main() {
	_ = godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "booking")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		logger.Error("pgxpool init failed", "err", err.Error())
		os.Exit(1)
	}
	defer pool.Close()

	// Point Service client — dùng cho DeductFee 20 EP khi Vựa nhận đơn.
	pointAddr := envOr("POINT_SERVICE_ADDR", "localhost:50053")
	pointConn, err := grpc.NewClient(pointAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("point client dial failed", "err", err.Error(), "addr", pointAddr)
		os.Exit(1)
	}
	defer pointConn.Close()
	pointClient := pointv1.NewPointServiceClient(pointConn)
	logger.Info("point service client ready", "addr", pointAddr)

	port := envOr("GRPC_PORT", "50052")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("listen failed", "err", err.Error(), "port", port)
		os.Exit(1)
	}

	srv := grpc.NewServer()
	bookingv1.RegisterBookingServiceServer(srv, grpcserver.NewBookingServer(pool, pointClient, logger))
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
	logger.Info("booking-service stopped")
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
