-- 0007: pickup points (ADR 0017). A PICKUP_POINT node collects packages for
-- shoppers; it is served by one delivery station and is operated by the
-- network or a partner. The serving station column exists exactly for a
-- pickup point; lane_rules and allowedPairs are untouched - a pickup point
-- appears in no pair, so it can never be a lane endpoint.
ALTER TABLE nodes
    ADD COLUMN serving_station_node_id TEXT
        CHECK ((node_type = 'PICKUP_POINT' AND serving_station_node_id IS NOT NULL)
            OR (node_type <> 'PICKUP_POINT' AND serving_station_node_id IS NULL));

ALTER TABLE nodes DROP CONSTRAINT nodes_node_type_check;
ALTER TABLE nodes
    ADD CONSTRAINT nodes_node_type_check
        CHECK (node_type IN ('FULFILLMENT_CENTER', 'INBOUND_CROSS_DOCK', 'RETURNS_CENTER',
                             'SORTATION_CENTER', 'DELIVERY_STATION', 'AIR_HUB', 'PICKUP_POINT'));

ALTER TABLE nodes DROP CONSTRAINT nodes_operator_check;
ALTER TABLE nodes
    ADD CONSTRAINT nodes_operator_check
        CHECK (operator IN ('NETWORK', 'VENDOR', 'PARTNER'));

-- PARTNER runs pickup points only (ADR 0017).
ALTER TABLE nodes
    ADD CONSTRAINT nodes_partner_is_pickup_point
        CHECK (node_type = 'PICKUP_POINT' OR operator <> 'PARTNER');

ALTER TABLE nodes
    ADD CONSTRAINT nodes_serving_station_fk
        FOREIGN KEY (serving_station_node_id) REFERENCES nodes (id);
