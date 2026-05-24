ALTER TABLE bookings
    DROP COLUMN IF EXISTS driver_pending_tx_id,
    DROP COLUMN IF EXISTS user_pending_tx_id;
