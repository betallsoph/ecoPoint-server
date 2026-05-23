package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/ecopoint/ecopoint/pkg/events"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // .env (nếu có) — silent khi vắng

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("service", "notification")
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	brokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := envOr("KAFKA_TOPIC_POINT_EVENTS", "point-events")
	groupID := envOr("KAFKA_GROUP_ID", "notify-group")

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10 << 20, // 10MB
		StartOffset:    kafka.LastOffset,
		ReadBackoffMin: 100 * time.Millisecond,
		ReadBackoffMax: 1 * time.Second,
		// CommitInterval = 0 (default) → ReadMessage tự commit offset đồng bộ.
	})

	// Graceful shutdown: SIGINT/SIGTERM → đóng reader → ReadMessage unblock.
	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, closing kafka reader")
		if err := reader.Close(); err != nil {
			logger.Warn("reader close failed", "err", err.Error())
		}
	}()

	logger.Info("notification-service consuming",
		"brokers", brokers,
		"topic", topic,
		"group_id", groupID,
	)

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			// Chỉ thoát khi context hủy thật sự (graceful shutdown).
			if errors.Is(err, context.Canceled) {
				break
			}
			// EOF/ErrClosedPipe xuất hiện khi topic chưa tồn tại (chưa ai publish).
			// Đây là hoàn cảnh bình thường lúc bootstrap — retry quiet, không exit.
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				logger.Error("kafka read failed", "err", err.Error())
			}
			select {
			case <-ctx.Done():
				// rơi ra ngoài → vòng lặp tiếp, ReadMessage sẽ trả ctx.Canceled → break.
			case <-time.After(time.Second):
			}
			continue
		}

		var evt events.PointAdded
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			logger.Error("unmarshal failed",
				"err", err.Error(),
				"raw", string(msg.Value),
				"offset", msg.Offset,
				"partition", msg.Partition,
			)
			continue
		}

		logger.Info("point added — sending notification",
			"user_id", evt.UserID,
			"points", evt.Points,
			"transaction_id", evt.TransactionID,
			"balance_after", evt.BalanceAfter,
			"source", evt.Source,
			"occurred_at", evt.OccurredAt,
			"offset", msg.Offset,
			"partition", msg.Partition,
		)
		// TODO: gọi service push notification / email / SMS.
	}

	logger.Info("notification-service stopped")
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
