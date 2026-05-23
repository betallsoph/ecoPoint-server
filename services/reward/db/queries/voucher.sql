-- name: GetVoucherByID :one
SELECT * FROM vouchers WHERE id = $1;

-- name: ReserveVoucherStock :one
-- Atomic decrement: chỉ trừ kho khi còn hàng & voucher đang active.
-- 0 dòng cập nhật ⇒ sqlc trả pgx.ErrNoRows ⇒ saga abort sớm.
UPDATE vouchers
SET stock = stock - 1,
    updated_at = NOW()
WHERE id = $1 AND stock > 0 AND is_active = TRUE
RETURNING *;

-- name: ReleaseVoucherStock :one
-- Compensation: hoàn 1 đơn vị kho khi Point Service từ chối trừ điểm.
UPDATE vouchers
SET stock = stock + 1,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: InsertPendingRedemption :one
INSERT INTO redemptions (voucher_id, user_id, point_cost, idempotency_key)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CompleteRedemption :one
UPDATE redemptions
SET status       = 'completed',
    point_tx_id  = $2,
    completed_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CancelRedemption :one
UPDATE redemptions
SET status         = 'cancelled',
    failure_reason = $2,
    cancelled_at   = NOW()
WHERE id = $1
RETURNING *;

-- name: GetRedemptionByIdempotencyKey :one
SELECT * FROM redemptions WHERE idempotency_key = $1;
