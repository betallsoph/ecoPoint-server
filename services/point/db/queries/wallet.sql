-- name: UpsertWallet :one
-- Tạo wallet nếu chưa có (idempotent).
INSERT INTO wallets (user_id, balance)
VALUES ($1, 0)
ON CONFLICT (user_id) DO UPDATE
    SET user_id = wallets.user_id
RETURNING *;

-- name: LockWallet :one
-- Khóa hàng wallet để cập nhật số dư an toàn dưới transaction.
SELECT *
FROM wallets
WHERE user_id = $1
FOR UPDATE;

-- name: UpdateBalance :one
-- Cập nhật số dư mới sau khi đã tính toán.
UPDATE wallets
SET balance    = $2,
    updated_at = NOW()
WHERE user_id = $1
RETURNING *;

-- name: InsertPointTransaction :one
INSERT INTO point_transactions (
    user_id, tx_type, amount, balance_after,
    source, reference_id, idempotency_key
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetTxByIdempotencyKey :one
SELECT *
FROM point_transactions
WHERE idempotency_key = $1;
