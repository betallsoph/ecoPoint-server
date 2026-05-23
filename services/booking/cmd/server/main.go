package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/joho/godotenv"
)

// Booking-service gRPC bootstrap.
// Proto BookingService chưa được khai báo trong shared/proto — service hiện
// chỉ wire-up DB pool + gRPC server rỗng để dev tiếp.
func main() {
	_ = godotenv.Load() // .env (nếu có) — silent khi vắng

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()
	_ = pool // sẽ dùng cho bookingdb.New(pool) khi handler hoàn thiện

	port := envOr("GRPC_PORT", "50052")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	srv := grpc.NewServer()
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		log.Println("[booking-service] shutting down")
		srv.GracefulStop()
	}()

	log.Printf("[booking-service] gRPC listening on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("missing env %s", k)
	}
	return v
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
