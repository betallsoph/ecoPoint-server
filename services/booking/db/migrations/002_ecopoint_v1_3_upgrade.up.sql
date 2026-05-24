-- ============================================================
-- db_booking :: v1.3 Master Blueprint — Vựa Arbitration + Anti-Fraud
--
--   1) bookings: thêm pin_code, pin_expired_at, proof_image_url,
--      driver_weight, collector_weight.
--   2) booking_status enum: + delivered_to_station, + reconciled.
--   3) Bảng mới: stations, station_members.
--
-- LƯU Ý cross-DB FK:
--   owner_id / user_id ở stations + station_members trỏ tới
--   users.id (db_user) — Postgres KHÔNG hỗ trợ FK ngang database
--   nên chỉ ràng buộc bằng UUID + check ở tầng app.
-- ============================================================

-- ─── 1. Thêm 2 status mới ─────────────────────────────────────
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'delivered_to_station';
ALTER TYPE booking_status ADD VALUE IF NOT EXISTS 'reconciled';

-- ─── 2. Cột mới trên bookings ─────────────────────────────────
ALTER TABLE bookings
    ADD COLUMN IF NOT EXISTS pin_code         VARCHAR(4),
    ADD COLUMN IF NOT EXISTS pin_expired_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS proof_image_url  TEXT,
    ADD COLUMN IF NOT EXISTS driver_weight    NUMERIC(10, 2),
    ADD COLUMN IF NOT EXISTS collector_weight NUMERIC(10, 2),
    -- Tracking Vựa nào nhận đơn này (FK soft → stations.id, cùng DB nên OK)
    ADD COLUMN IF NOT EXISTS station_id       UUID;

-- PIN phải đủ 4 digit nếu set
ALTER TABLE bookings
    ADD CONSTRAINT bookings_pin_format_chk
        CHECK (pin_code IS NULL OR pin_code ~ '^[0-9]{4}$');

-- Cân không âm
ALTER TABLE bookings
    ADD CONSTRAINT bookings_driver_weight_chk
        CHECK (driver_weight IS NULL OR driver_weight >= 0),
    ADD CONSTRAINT bookings_collector_weight_chk
        CHECK (collector_weight IS NULL OR collector_weight >= 0);

-- Idx để reconcile job quét đơn đang chờ Vựa (delivered_to_station)
CREATE INDEX IF NOT EXISTS bookings_pending_station_idx
    ON bookings (created_at)
 WHERE status = 'delivered_to_station';

-- ─── 3. Bảng stations (Vựa) ───────────────────────────────────
CREATE TABLE IF NOT EXISTS stations (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL,
    address     TEXT         NOT NULL,
    -- Toạ độ Vựa (optional ở Phase 1; sẽ dùng để route đơn ở Phase 2).
    location    GEOMETRY(Point, 4326),
    owner_id    UUID         NOT NULL,     -- ref users.id (cross-DB, không FK)
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS stations_owner_idx    ON stations (owner_id);
CREATE INDEX IF NOT EXISTS stations_location_gix ON stations USING GIST (location);

-- ─── 4. Bảng station_members ──────────────────────────────────
-- Composite PK (station_id, user_id) — 1 user chỉ thuộc 1 station
-- 1 lần. role nội bộ (admin/staff) giữ ở users.role để JWT carry.
CREATE TABLE IF NOT EXISTS station_members (
    station_id UUID        NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL,         -- ref users.id (cross-DB)
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (station_id, user_id)
);

CREATE INDEX IF NOT EXISTS station_members_user_idx ON station_members (user_id);

-- ─── 5. FK nội-DB: bookings.station_id → stations.id ──────────
-- An toàn vì cả 2 cùng db_booking.
ALTER TABLE bookings
    ADD CONSTRAINT bookings_station_id_fk
        FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE SET NULL;
