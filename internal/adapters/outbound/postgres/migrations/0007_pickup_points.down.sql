-- 0007 down: pickup points (ADR 0017).
ALTER TABLE nodes DROP CONSTRAINT nodes_serving_station_fk;
ALTER TABLE nodes DROP CONSTRAINT nodes_partner_is_pickup_point;
ALTER TABLE nodes DROP CONSTRAINT nodes_operator_check;
ALTER TABLE nodes
    ADD CONSTRAINT nodes_operator_check CHECK (operator IN ('NETWORK', 'VENDOR'));
ALTER TABLE nodes DROP CONSTRAINT nodes_node_type_check;
ALTER TABLE nodes
    ADD CONSTRAINT nodes_node_type_check
        CHECK (node_type IN ('FULFILLMENT_CENTER', 'INBOUND_CROSS_DOCK', 'RETURNS_CENTER',
                             'SORTATION_CENTER', 'DELIVERY_STATION', 'AIR_HUB'));
ALTER TABLE nodes DROP COLUMN serving_station_node_id;
