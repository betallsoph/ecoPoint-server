-- name: CreateBooking :one
INSERT INTO bookings (
    customer_id, address, location, estimated_kg, material_type, note, scheduled_at
) VALUES (
    $1,
    $2,
    ST_SetSRID(ST_MakePoint($3, $4), 4326),
    $5, $6, $7, $8
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
    material_type,
    note,
    scheduled_at,
    created_at,
    updated_at;

-- name: ListMyBookings :many
SELECT
    id,
    customer_id,
    collector_id,
    status,
    address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg,
    material_type,
    note,
    scheduled_at,
    created_at,
    updated_at
FROM bookings
WHERE customer_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListBookings :many
SELECT
    id,
    customer_id,
    collector_id,
    status,
    address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg,
    material_type,
    note,
    scheduled_at,
    created_at,
    updated_at
FROM bookings
ORDER BY created_at DESC
LIMIT $1;

-- name: ListBookingsByStatus :many
SELECT
    id,
    customer_id,
    collector_id,
    status,
    address,
    ST_X(location)::float8 AS longitude,
    ST_Y(location)::float8 AS latitude,
    estimated_kg,
    material_type,
    note,
    scheduled_at,
    created_at,
    updated_at
FROM bookings
WHERE status = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: FindNearestBookings :many
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
    material_type,
    note,
    scheduled_at,
    created_at,
    updated_at
FROM bookings
WHERE status = 'pending'
ORDER BY location <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)
LIMIT $3;
