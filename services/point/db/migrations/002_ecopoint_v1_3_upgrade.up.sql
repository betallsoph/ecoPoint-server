-- ============================================================
-- db_point :: v1.3 Master Blueprint — Two-Phase Ledger
--
--   1) NUMERIC → BIGINT cho mọi cột tiền (1 EP = 100 VNĐ, INT).
--   2) wallets.balance → balance_available + thêm balance_pending.
--   3) point_transactions.status (PENDING/AVAILABLE/CANCELLED).
--
-- Tất cả ALTER giữ data cũ (ROUND khi cast Numeric → BIGINT).
-- Lưu ý: row cũ tạo trước v1.3 được mark status='available' (đã
-- settle xong từ trước, không cần đợi Vựa xác nhận).
-- ============================================================

-- ─── 1. wallets ────────────────────────────────────────────────
ALTER TABLE wallets
    DROP CONSTRAINT IF EXISTS wallets_balance_check;

ALTER TABLE wallets RENAME COLUMN balance TO balance_available;

ALTER TABLE wallets
    ALTER COLUMN balance_available TYPE BIGINT
        USING ROUND(balance_available)::BIGINT,
    ALTER COLUMN balance_available SET DEFAULT 0;

ALTER TABLE wallets
    ADD COLUMN IF NOT EXISTS balance_pending BIGINT NOT NULL DEFAULT 0;

ALTER TABLE wallets
    ADD CONSTRAINT wallets_balance_available_chk
        CHECK (balance_available >= 0),
    ADD CONSTRAINT wallets_balance_pending_chk
        CHECK (balance_pending   >= 0);

-- ─── 2. point_transactions ────────────────────────────────────
-- Bỏ CHECK cũ trên amount (NUMERIC > 0) trước khi cast.
ALTER TABLE point_transactions
    DROP CONSTRAINT IF EXISTS point_transactions_amount_check;

ALTER TABLE point_transactions
    ALTER COLUMN amount        TYPE BIGINT USING ROUND(amount)::BIGINT,
    ALTER COLUMN balance_after TYPE BIGINT USING ROUND(balance_after)::BIGINT;

ALTER TABLE point_transactions
    ADD CONSTRAINT point_transactions_amount_positive
        CHECK (amount > 0);

-- ─── 3. status (PENDING/AVAILABLE/CANCELLED) ──────────────────
ALTER TABLE point_transactions
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'available', 'cancelled'));

-- Backfill: tất cả row cũ coi như đã settle.
UPDATE point_transactions
   SET status = 'available'
 WHERE status = 'pending';

-- Index cho job reconcile quét pending quá hạn (Vựa chưa xác nhận).
CREATE INDEX IF NOT EXISTS point_tx_pending_idx
    ON point_transactions (created_at)
 WHERE status = 'pending';
