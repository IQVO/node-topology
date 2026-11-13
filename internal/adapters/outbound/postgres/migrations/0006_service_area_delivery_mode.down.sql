-- 0006 down: the delivery mode and its carrier options leave with the
-- columns; every area becomes the NETWORK_ROUTE area it was before.
ALTER TABLE service_areas
    DROP COLUMN carrier_options,
    DROP COLUMN delivery_mode;
