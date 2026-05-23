-- ============================================================
-- Ecopoint :: reward-service schema
-- DB: db_reward
-- ============================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE redemption_status AS ENUM ('pending', 'completed', 'cancelled');

-- Catalog phần thưởng
CREATE TABLE vouchers (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    code        VARCHAR(64)     NOT NULL UNIQUE,
    title       VARCHAR(255)    NOT NULL,
    description TEXT,
    point_cost  NUMERIC(20, 4)  NOT NULL CHECK (point_cost > 0),
    stock       INTEGER         NOT NULL CHECK (stock >= 0),
    is_active   BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Sổ cái đổi quà (audit log + state machine của Saga)
CREATE TABLE redemptions (
    id              UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    voucher_id      UUID              NOT NULL REFERENCES vouchers(id),
    user_id         UUID              NOT NULL,
    point_cost      NUMERIC(20, 4)    NOT NULL,
    status          redemption_status NOT NULL DEFAULT 'pending',
    point_tx_id     VARCHAR(255),     -- transaction ID trả về từ Point Service khi deduct OK
    failure_reason  TEXT,             -- ghi lý do khi rollback
    idempotency_key VARCHAR(255)      NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    cancelled_at    TIMESTAMPTZ
);

CREATE INDEX redemptions_user_created_idx ON redemptions (user_id, created_at DESC);
CREATE INDEX redemptions_voucher_idx      ON redemptions (voucher_id);
-- Partial index giúp job reconcile quét nhanh các saga đang treo.
CREATE INDEX redemptions_pending_idx      ON redemptions (created_at) WHERE status = 'pending';
