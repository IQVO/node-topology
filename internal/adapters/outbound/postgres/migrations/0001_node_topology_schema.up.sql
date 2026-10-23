-- node-topology core schema: one table per aggregate plus the append-only
-- profile revision history of a node.
--
-- Every aggregate row carries `version`, the optimistic-concurrency counter of
-- the aggregate (a Save UPDATEs ... WHERE version = <loaded version>). Ids are
-- opaque text; ordering by id is byte-wise (COLLATE "C") in the queries.

-- regions: a partition of the network. The code is unique (ErrRegionCodeTaken).
CREATE TABLE regions (
    id         TEXT        PRIMARY KEY,
    code       TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    defined_at TIMESTAMPTZ NOT NULL,
    version    BIGINT      NOT NULL CHECK (version >= 1),
    CONSTRAINT regions_code_key UNIQUE (code)
);

-- nodes: the CURRENT snapshot of each building. profile_revision points at the
-- highest row of node_profile_revisions (NULL until a profile is declared).
CREATE TABLE nodes (
    id               TEXT             PRIMARY KEY,
    node_type        TEXT             NOT NULL CHECK (node_type IN (
                         'FULFILLMENT_CENTER', 'INBOUND_CROSS_DOCK', 'RETURNS_CENTER',
                         'SORTATION_CENTER', 'DELIVERY_STATION', 'AIR_HUB')),
    subtype          TEXT,
    operator         TEXT             NOT NULL CHECK (operator IN ('NETWORK', 'VENDOR')),
    vendor_id        TEXT,
    vendor_node_code TEXT,
    region_id        TEXT             NOT NULL REFERENCES regions (id),
    timezone         TEXT             NOT NULL,
    postal_code      TEXT             NOT NULL,
    country_code     TEXT             NOT NULL,
    lat              DOUBLE PRECISION,
    lon              DOUBLE PRECISION,
    status           TEXT             NOT NULL CHECK (status IN ('PROPOSED', 'ACTIVE', 'SUSPENDED', 'RETIRED')),
    profile_revision INTEGER          CHECK (profile_revision >= 1),
    activated_at     TIMESTAMPTZ,
    suspended_at     TIMESTAMPTZ,
    suspend_reason   TEXT,
    retired_at       TIMESTAMPTZ,
    version          BIGINT           NOT NULL CHECK (version >= 1),
    CONSTRAINT nodes_vendor_ref_complete CHECK ((vendor_id IS NULL) = (vendor_node_code IS NULL)),
    CONSTRAINT nodes_geo_complete CHECK ((lat IS NULL) = (lon IS NULL))
);

-- (vendorId, vendorNodeCode) is unique per vendor (ErrVendorNodeCodeTaken);
-- network-run nodes have no vendor reference and are not constrained.
CREATE UNIQUE INDEX nodes_vendor_ref_key ON nodes (vendor_id, vendor_node_code) WHERE vendor_id IS NOT NULL;
CREATE INDEX nodes_region_id_idx ON nodes (region_id);
CREATE INDEX nodes_type_status_idx ON nodes (node_type, status);

-- node_profile_revisions: every declared operational profile, one immutable
-- row per (node, revision). Rows are only ever inserted.
CREATE TABLE node_profile_revisions (
    node_id               TEXT        NOT NULL REFERENCES nodes (id),
    revision              INTEGER     NOT NULL CHECK (revision >= 1),
    timezone              TEXT        NOT NULL,
    operating_hours       JSONB       NOT NULL,
    holidays              JSONB       NOT NULL,
    handling_time_minutes INTEGER     NOT NULL CHECK (handling_time_minutes >= 0),
    cutoffs               JSONB       NOT NULL,
    throughput_value      INTEGER     NOT NULL CHECK (throughput_value >= 0),
    throughput_time_unit  TEXT        NOT NULL CHECK (throughput_time_unit IN ('HOUR', 'DAY')),
    throughput_unit       TEXT        NOT NULL,
    sort_dwell_minutes    INTEGER     CHECK (sort_dwell_minutes >= 0),
    declared_by           TEXT        NOT NULL CHECK (declared_by IN ('VENDOR', 'NETWORK_OPS')),
    declared_at           TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (node_id, revision)
);

CREATE FUNCTION node_profile_revisions_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'node_profile_revisions is append-only (% on node %, revision %)',
        TG_OP, OLD.node_id, OLD.revision USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER node_profile_revisions_append_only
    BEFORE UPDATE OR DELETE ON node_profile_revisions
    FOR EACH ROW EXECUTE FUNCTION node_profile_revisions_append_only();

-- The node row always points at a stored revision. Deferred, because the node
-- row is written before the revision row inside the same transaction.
ALTER TABLE nodes ADD CONSTRAINT nodes_current_profile_fkey
    FOREIGN KEY (id, profile_revision) REFERENCES node_profile_revisions (node_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

-- lanes: directed, mode-specific connections. At most one OPEN lane per
-- (origin, destination, mode) (ErrDuplicateLane); closed lanes are history.
CREATE TABLE lanes (
    id                      TEXT        PRIMARY KEY,
    origin_node_id          TEXT        NOT NULL REFERENCES nodes (id),
    destination_node_id     TEXT        NOT NULL REFERENCES nodes (id),
    mode                    TEXT        NOT NULL CHECK (mode IN ('GROUND', 'AIR')),
    nominal_transit_minutes INTEGER     NOT NULL CHECK (nominal_transit_minutes >= 1),
    status                  TEXT        NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    opened_at               TIMESTAMPTZ NOT NULL,
    closed_at               TIMESTAMPTZ,
    version                 BIGINT      NOT NULL CHECK (version >= 1),
    CONSTRAINT lanes_no_loop CHECK (origin_node_id <> destination_node_id),
    CONSTRAINT lanes_closed_at_matches_status CHECK ((status = 'CLOSED') = (closed_at IS NOT NULL))
);

CREATE UNIQUE INDEX lanes_one_open_key ON lanes (origin_node_id, destination_node_id, mode) WHERE status = 'OPEN';
CREATE INDEX lanes_destination_idx ON lanes (destination_node_id);

-- service_areas: which delivery station serves a postal prefix, matched exactly.
CREATE TABLE service_areas (
    postal_prefix            TEXT        PRIMARY KEY,
    delivery_station_node_id TEXT        NOT NULL REFERENCES nodes (id),
    region_id                TEXT        NOT NULL REFERENCES regions (id),
    assignment_version       INTEGER     NOT NULL CHECK (assignment_version >= 1),
    assigned_at              TIMESTAMPTZ NOT NULL,
    version                  BIGINT      NOT NULL CHECK (version >= 1)
);

CREATE INDEX service_areas_station_idx ON service_areas (delivery_station_node_id);
CREATE INDEX service_areas_region_idx ON service_areas (region_id);
