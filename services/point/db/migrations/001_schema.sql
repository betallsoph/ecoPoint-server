-- ============================================================
-- Ecopoint :: point-service schema
-- DB: db_point
-- ============================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Loại bút toán
CREATE TYPE point_tx_type AS ENUM ('add', 'deduct');

-- Số dư điểm của user
CREATE TABLE wallets (
    user_id     UUID PRIMARY KEY,
    balance     NUMERIC(20, 4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- Lịch sử bút toán (append-only)
CREATE TABLE point_transactions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID           NOT NULL REFERENCES wallets(user_id),
    tx_type          point_tx_type  NOT NULL,
    amount           NUMERIC(20, 4) NOT NULL CHECK (amount > 0),
    balance_after    NUMERIC(20, 4) NOT NULL,
    source           VARCHAR(64),
    reference_id     VARCHAR(255),
    idempotency_key  VARCHAR(255)   NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX point_tx_user_created_idx
    ON point_transactions (user_id, created_at DESC);
