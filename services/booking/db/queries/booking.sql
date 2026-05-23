-- name: CreateBooking :one
-- Truyền vào: customer_id, address, longitude, latitude, estimated_kg, note, scheduled_at.
INSERT INTO bookings (
    customer_id, address, location, estimated_kg, note, scheduled_at
) VALUES (
    $1,
    $2,
    ST_SetSRID(ST_MakePoint($3, $4), 4326),
    $5, $6, $7
)
RETURNING
    id,
    customer_id,
    collector_id,
    status,
    address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg,
    note,
    scheduled_at,
    created_at,
    updated_at;

-- name: FindNearestBookings :many
-- Tìm 5 booking 'pending' gần nhất với toạ độ ($1 = longitude, $2 = latitude).
-- Dùng toán tử KNN `<->` để tận dụng GIST index, sau đó tính khoảng cách thực tế
-- bằng ST_DistanceSphere (đơn vị: mét, độ chính xác cao trên bề mặt cầu).
SELECT
    id,
    customer_id,
    collector_id,
    status,
    address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    ST_DistanceSphere(
        location,
        ST_SetSRID(ST_MakePoint($1, $2), 4326)
    )::float8 AS distance_m,
    estimated_kg,
    scheduled_at,
    created_at
FROM bookings
WHERE status = 'pending'
ORDER BY location <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)
LIMIT 5;
