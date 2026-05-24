-- ============================================================
-- db_point :: rollback v1.3 Two-Phase Ledger
--
-- Cast BIGINT → NUMERIC(20,4) an toàn (giá trị nguyên fit hoàn
-- toàn vào numeric scale 4). balance_pending bị xoá nên TỔNG ví
-- = chỉ còn available — đảm bảo không double-count.
-- ============================================================

-- ─── point_transactions ───────────────────────────────────────
DROP INDEX IF EXISTS point_tx_pending_idx;

ALTER TABLE point_transactions DROP COLUMN IF EXISTS status;

ALTER TABLE point_transactions
    DROP CONSTRAINT IF EXISTS point_transactions_amount_positive;

ALTER TABLE point_transactions
    ALTER COLUMN balance_after TYPE NUMERIC(20, 4) USING balance_after::NUMERIC,
    ALTER COLUMN amount        TYPE NUMERIC(20, 4) USING amount::NUMERIC;

ALTER TABLE point_transactions
    ADD CONSTRAINT point_transactions_amount_check CHECK (amount > 0);

-- ─── wallets ───────────────────────────────────────────────────
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_balance_pending_chk;
ALTER TABLE wallets DROP CONSTRAINT IF EXISTS wallets_balance_available_chk;

ALTER TABLE wallets DROP COLUMN IF EXISTS balance_pending;

ALTER TABLE wallets
    ALTER COLUMN balance_available TYPE NUMERIC(20, 4) USING balance_available::NUMERIC,
    ALTER COLUMN balance_available SET DEFAULT 0;

ALTER TABLE wallets RENAME COLUMN balance_available TO balance;

ALTER TABLE wallets
    ADD CONSTRAINT wallets_balance_check CHECK (balance >= 0);
