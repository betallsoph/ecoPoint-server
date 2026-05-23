package events

import "time"

// Topic publish khi Saga đổi voucher hoàn tất thành công.
const TopicRewardEvents = "reward-events"

// RewardRedeemed — payload bắn vào `reward-events` sau khi Point Service trừ điểm OK.
type RewardRedeemed struct {
	RedemptionID string    `json:"redemptionId"`
	UserID       string    `json:"userId"`
	VoucherID    string    `json:"voucherId"`
	PointCost    string    `json:"pointCost"`
	PointTxID    string    `json:"pointTxId"`
	OccurredAt   time.Time `json:"occurredAt"`
}
