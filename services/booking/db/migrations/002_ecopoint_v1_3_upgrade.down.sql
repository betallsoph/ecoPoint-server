-- ============================================================
-- db_booking :: rollback v1.3 upgrade
--
-- Postgres KHÔNG hỗ trợ DROP VALUE enum → delivered_to_station /
-- reconciled tồn tại vĩnh viễn. Block recreate enum ở dưới
-- (comment-out) để dùng khi chắc chắn không còn row dùng 2 value
-- đó.
-- ============================================================

-- ─── FK bookings.station_id ───────────────────────────────────
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_station_id_fk;

-- ─── Bảng members trước, stations sau ─────────────────────────
DROP INDEX IF EXISTS station_members_user_idx;
DROP TABLE IF EXISTS station_members;

DROP INDEX IF EXISTS stations_location_gix;
DROP INDEX IF EXISTS stations_owner_idx;
DROP TABLE IF EXISTS stations;

-- ─── Cột mới trên bookings ────────────────────────────────────
DROP INDEX IF EXISTS bookings_pending_station_idx;

ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_collector_weight_chk;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_driver_weight_chk;
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_pin_format_chk;

ALTER TABLE bookings
    DROP COLUMN IF EXISTS station_id,
    DROP COLUMN IF EXISTS collector_weight,
    DROP COLUMN IF EXISTS driver_weight,
    DROP COLUMN IF EXISTS proof_image_url,
    DROP COLUMN IF EXISTS pin_expired_at,
    DROP COLUMN IF EXISTS pin_code;

-- ─── Enum values (TUỲ CHỌN) ───────────────────────────────────
-- Postgres không DROP VALUE được. Nếu thật sự cần dọn:
--
-- ALTER TABLE bookings
--     ALTER COLUMN status TYPE TEXT USING status::TEXT;
-- DROP TYPE booking_status;
-- CREATE TYPE booking_status AS ENUM
--     ('pending', 'accepted', 'collecting', 'completed', 'cancelled');
-- UPDATE bookings SET status = 'completed'
--   WHERE status IN ('delivered_to_station', 'reconciled');
-- ALTER TABLE bookings
--     ALTER COLUMN status TYPE booking_status USING status::booking_status,
--     ALTER COLUMN status SET DEFAULT 'pending';
