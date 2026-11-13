-- 0006: service-area delivery mode (ADR 0016). An area's last mile is served
-- either by the network's own route (the default, and the behaviour every
-- existing row keeps) or by a handoff to an external carrier. The mode is a
-- planner DECLARATION; the per-package carrier choice is computed downstream
-- from the declared options, never stored.
ALTER TABLE service_areas
    ADD COLUMN delivery_mode TEXT NOT NULL DEFAULT 'NETWORK_ROUTE'
        CHECK (delivery_mode IN ('NETWORK_ROUTE', 'CARRIER_HANDOFF')),
    ADD COLUMN carrier_options JSONB NOT NULL DEFAULT '[]';
