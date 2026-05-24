-- ============================================================
-- db_booking :: V1.3 — lưu tx_id của 2 IssuePendingReward
--
-- DriverCompleteBooking gọi Point.IssuePendingReward 2 lần (User + Driver)
-- → cần lưu tx_id để CollectorVerifyBooking có thể Confirm/Cancel sau.
-- Lưu trực tiếp trên booking row để tránh thêm RPC tra cứu.
-- ============================================================

ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS user_pending_tx_id   UUID,
    ADD COLUMN IF NOT EXISTS driver_pending_tx_id UUID;
