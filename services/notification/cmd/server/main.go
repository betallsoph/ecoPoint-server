package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/ecopoint/ecopoint/pkg/events"
	"github.com/ecopoint/ecopoint/pkg/mq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "notification")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	amqpURL := envOr("RABBITMQ_URL", "amqp://ecopoint:ecopoint_secret@localhost:5672/")
	queueName := envOr("QUEUE_NAME", "notification.point_events")
	prefetch, _ := strconv.Atoi(envOr("PREFETCH", "16"))

	conn, err := mq.Dial(ctx, amqpURL, logger)
	if err != nil {
		logger.Error("rabbitmq dial failed", "err", err.Error())
		os.Exit(1)
	}

	consumer := mq.NewConsumer(conn, events.ExchangePointEvents, queueName, prefetch, logger, handle(logger))

	// Run blocks; khi ctx tắt → consumer drain → return.
	logger.Info("notification-service ready")
	consumer.Run(ctx)

	// Đóng connection sau khi consumer dừng để đảm bảo ack được flush.
	conn.Close()
	logger.Info("notification-service stopped")
}

func handle(logger *slog.Logger) mq.Handler {
	return func(_ context.Context, delivery amqp.Delivery) error {
		var evt events.PointAdded
		if err := json.Unmarshal(delivery.Body, &evt); err != nil {
			logger.Error("unmarshal failed", "err", err.Error(), "body", string(delivery.Body))
			// Trả nil → ACK để không kẹt poison message. Production nên route sang DLQ.
			return nil
		}

		logger.Info("point added — sending notification",
			"user_id", evt.UserID,
			"points", evt.Points,
			"transaction_id", evt.TransactionID,
			"balance_after", evt.BalanceAfter,
			"source", evt.Source,
			"occurred_at", evt.OccurredAt,
		)
		// TODO: gọi service push notification / email / SMS.
		return nil
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
