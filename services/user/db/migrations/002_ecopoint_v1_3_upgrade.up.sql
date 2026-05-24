-- ============================================================
-- db_user :: v1.3 Master Blueprint
--   - Trust Layer: thêm cột trust_score vào users
--   - Role enum: thêm user / station_admin / station_staff
--   - Device fingerprinting: bảng user_devices
--
-- KHÔNG đụng dữ liệu cũ: chỉ thêm cột / value mới. Migration code
-- (Drizzle schema + Go/TS auth handler) sẽ làm ở bước sau.
-- ============================================================

-- ─── 1. Thêm giá trị enum mới ─────────────────────────────────
-- Postgres không cho ADD VALUE trong transaction nếu rồi dùng giá
-- trị đó NGAY trong cùng statement; nhưng file migration chạy
-- autocommit nên mỗi ALTER TYPE commit ngay, statement sau có thể
-- dùng được.
ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'user';
ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'station_admin';
ALTER TYPE user_role ADD VALUE IF NOT EXISTS 'station_staff';

-- LƯU Ý: KHÔNG migrate dữ liệu cũ ở bước này (customer → user,
-- admin → station_admin) vì app Go/TS hiện đang map theo enum cũ.
-- Đợi sang bước refactor code mới UPDATE rows.

-- ─── 2. Trust Score ───────────────────────────────────────────
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS trust_score INT NOT NULL DEFAULT 100;

ALTER TABLE users
    ADD CONSTRAINT users_trust_score_chk CHECK (trust_score BETWEEN 0 AND 1000);

CREATE INDEX IF NOT EXISTS users_trust_score_idx ON users (trust_score)
    WHERE trust_score < 50; -- partial idx: chỉ cần quét user "đáng ngờ"

-- ─── 3. Device fingerprinting ─────────────────────────────────
-- Hardware-ID 1 user gắn N device, nhưng 1 device_id duy nhất
-- toàn hệ thống. Logic "max 2 user/device" được enforce ở app
-- bằng cách check COUNT(*) trước khi INSERT.
CREATE TABLE IF NOT EXISTS user_devices (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id   VARCHAR(255) NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS user_devices_user_idx ON user_devices (user_id);
