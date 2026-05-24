-- ============================================================
-- db_user :: rollback v1.3 upgrade
--
-- Postgres KHÔNG hỗ trợ DROP VALUE trên ENUM → user/station_admin/
-- station_staff sẽ tồn tại vĩnh viễn trong type. Muốn xoá thật:
-- phải recreate type rồi cast lại cột (block ở dưới, comment-out).
-- ============================================================

-- ─── Device fingerprinting ────────────────────────────────────
DROP INDEX IF EXISTS user_devices_user_idx;
DROP TABLE IF EXISTS user_devices;

-- ─── Trust score ──────────────────────────────────────────────
DROP INDEX IF EXISTS users_trust_score_idx;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_trust_score_chk;
ALTER TABLE users DROP COLUMN IF EXISTS trust_score;

-- ─── Enum values (TUỲ CHỌN — chạy khi chắc chắn không còn row) ─
-- BLOCK dưới đây sẽ recreate user_role nếu cần huỷ hẳn các value
-- mới. Bỏ comment để dùng.
--
-- ALTER TABLE users
--     ALTER COLUMN role TYPE TEXT USING role::TEXT;
-- DROP TYPE user_role;
-- CREATE TYPE user_role AS ENUM ('customer', 'collector', 'admin');
-- ALTER TABLE users
--     ALTER COLUMN role TYPE user_role USING role::user_role;
