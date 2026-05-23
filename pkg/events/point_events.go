package events

import "time"

// Exchange & routing key dùng chung giữa point-service và notification-service.
const (
	ExchangePointEvents = "point_events"
	RoutingKeyAdded     = "point.added"
)

// PointAdded — payload publish lên `point_events` sau khi AddPoints commit.
// Số điểm để dạng string nhằm giữ độ chính xác như Decimal trong proto.
type PointAdded struct {
	UserID        string    `json:"userId"`
	Points        string    `json:"points"`
	TransactionID string    `json:"transactionId"`
	BalanceAfter  string    `json:"balanceAfter"`
	Source        string    `json:"source,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
}
