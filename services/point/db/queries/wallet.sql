-- ============================================================
-- Point Service queries — V1.3 Two-Phase Ledger (int64)
-- ============================================================

-- ─── Wallet helpers ───────────────────────────────────────────

-- name: UpsertWallet :one
-- Tạo ví rỗng nếu chưa có (idempotent).
INSERT INTO wallets (user_id, balance_available, balance_pending)
VALUES ($1, 0, 0)
ON CONFLICT (user_id) DO UPDATE
    SET user_id = wallets.user_id
RETURNING *;

-- name: GetWallet :one
SELECT * FROM wallets WHERE user_id = $1;

-- name: LockWallet :one
-- SELECT … FOR UPDATE — lock row trước khi cập nhật trong tx.
SELECT * FROM wallets WHERE user_id = $1 FOR UPDATE;

-- ─── Transaction helpers ──────────────────────────────────────

-- name: GetTxByIdempotencyKey :one
SELECT * FROM point_transactions WHERE idempotency_key = $1;

-- name: GetTxByID :one
SELECT * FROM point_transactions WHERE id = $1;

-- ============================================================
-- 1) DeductFee — trừ THẲNG balance_available
--    Dùng cho: phí 20 EP của Vựa, redeem voucher, các loại phí
--    platform khác. Tiền đã settle, không qua phase pending.
-- ============================================================

-- name: DeductAvailable :one
-- Trừ available; trả 0 row nếu thiếu tiền → caller xử lý fail.
UPDATE wallets
SET balance_available = balance_available - $2,
    updated_at        = NOW()
WHERE user_id = $1 AND balance_available >= $2
RETURNING *;

-- name: InsertDeductFeeTx :one
INSERT INTO point_transactions (
    user_id, tx_type, amount, balance_after,
    source, reference_id, idempotency_key, status
) VALUES (
    $1, 'deduct', $2, $3,
    $4, $5, $6, 'available'
)
RETURNING *;

-- ============================================================
-- 2) IssuePendingReward — cộng vào balance_pending
--    Driver chốt đơn → tạo điểm chờ Vựa xác nhận. Chưa khả dụng.
-- ============================================================

-- name: AddPending :one
UPDATE wallets
SET balance_pending = balance_pending + $2,
    updated_at      = NOW()
WHERE user_id = $1
RETURNING *;

-- name: InsertPendingRewardTx :one
INSERT INTO point_transactions (
    user_id, tx_type, amount, balance_after,
    source, reference_id, idempotency_key, status
) VALUES (
    $1, 'add', $2, $3,
    $4, $5, $6, 'pending'
)
RETURNING *;

-- ============================================================
-- 3) ConfirmReward — pending → available (Vựa xác nhận nhập kho)
-- ============================================================

-- name: ConfirmPendingBalance :one
-- Trừ pending + cộng available CÙNG LÚC (atomic trong 1 UPDATE).
UPDATE wallets
SET balance_pending   = balance_pending - $2,
    balance_available = balance_available + $2,
    updated_at        = NOW()
WHERE user_id = $1 AND balance_pending >= $2
RETURNING *;

-- name: MarkTxAvailable :one
UPDATE point_transactions
SET status = 'available'
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- ============================================================
-- 4) CancelReward — pending → cancelled (Vựa phát hiện gian lận)
-- ============================================================

-- name: CancelPendingBalance :one
UPDATE wallets
SET balance_pending = balance_pending - $2,
    updated_at      = NOW()
WHERE user_id = $1 AND balance_pending >= $2
RETURNING *;

-- name: MarkTxCancelled :one
UPDATE point_transactions
SET status = 'cancelled'
WHERE id = $1 AND status = 'pending'
RETURNING *;
