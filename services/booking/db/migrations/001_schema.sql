-- ============================================================
-- Ecopoint :: booking-service schema
-- DB: db_booking (đã bật PostGIS từ init script)
-- ============================================================

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TYPE booking_status AS ENUM (
    'pending',
    'accepted',
    'collecting',
    'completed',
    'cancelled'
);

CREATE TABLE bookings (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id    UUID            NOT NULL,
    collector_id   UUID,
    status         booking_status  NOT NULL DEFAULT 'pending',

    address        TEXT            NOT NULL,
    -- Tọa độ điểm thu gom; SRID 4326 = WGS84 (lng/lat chuẩn GPS).
    location       GEOMETRY(Point, 4326) NOT NULL,

    estimated_kg   NUMERIC(10, 2),
    note           TEXT,
    scheduled_at   TIMESTAMPTZ,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- GIST index cho truy vấn không gian (KNN, ST_DWithin, …)
CREATE INDEX bookings_location_gix ON bookings USING GIST (location);

-- B-tree phụ cho filter trạng thái phổ biến
CREATE INDEX bookings_status_idx ON bookings (status);
