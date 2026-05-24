-- ============================================================
-- Booking Service queries — V1.3
-- ============================================================

-- name: CreateBooking :one
-- Tham số: customer_id, address, longitude, latitude, estimated_kg,
--         material_type, note, scheduled_at, pin_code, pin_expired_at.
-- PIN 4-digit + TTL được generate ở app layer (crypto/rand).
INSERT INTO bookings (
    customer_id, address, location, estimated_kg, material_type,
    note, scheduled_at, pin_code, pin_expired_at
) VALUES (
    $1, $2,
    ST_SetSRID(ST_MakePoint($3, $4), 4326),
    $5, $6, $7, $8, $9, $10
)
RETURNING
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at;

-- name: GetBookingByID :one
SELECT
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at
FROM bookings
WHERE id = $1;

-- name: AcceptBookingByStation :one
-- Atomic reservation: chỉ chuyển PENDING → ACCEPTED khi chưa ai nhận
-- (chống race 2 Vựa cùng grab 1 đơn). 0 row → bookings không thay đổi.
UPDATE bookings
SET status     = 'accepted',
    station_id = $2,
    updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at;

-- name: RollbackBookingAccept :exec
-- Hoàn về PENDING nếu DeductFee fail (compensation).
UPDATE bookings
SET status     = 'pending',
    station_id = NULL,
    updated_at = NOW()
WHERE id = $1 AND status = 'accepted' AND station_id = $2;

-- ─── Station ──────────────────────────────────────────────────

-- name: GetStation :one
SELECT id, name, address, owner_id, is_active, created_at, updated_at
FROM stations
WHERE id = $1;

-- ─── List queries (refresh RETURNING với cột mới) ─────────────

-- name: ListMyBookings :many
SELECT
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at
FROM bookings
WHERE customer_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListBookings :many
SELECT
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at
FROM bookings
ORDER BY created_at DESC
LIMIT $1;

-- name: ListBookingsByStatus :many
SELECT
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at
FROM bookings
WHERE status = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: FindNearestBookings :many
SELECT
    id, customer_id, collector_id, status, address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    ST_DistanceSphere(
        location,
        ST_SetSRID(ST_MakePoint($1, $2), 4326)
    )::float8 AS distance_m,
    estimated_kg, material_type, note, scheduled_at,
    created_at, updated_at, station_id,
    pin_code, pin_expired_at
FROM bookings
WHERE status = 'pending'
ORDER BY location <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)
LIMIT $3;
