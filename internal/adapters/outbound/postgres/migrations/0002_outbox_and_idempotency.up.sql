-- outbox_events: the transactional outbox (same shape as the fleet siblings).
-- One row per already-encoded Kafka message, inserted in the SAME transaction
-- as the aggregate rows; the relay (a later step) drains unpublished rows.
--   event_id    the CloudEvents `id`, minted once at append and persisted: a
--               relay retry republishes the same id.
--   topic       retailnetwork.node-topology.events | .analytics
--   event_type  the FULL CloudEvents `type`
--               (com.retailnetwork.<tier>.node-topology.<entity>.<EventName>).
--   subject     the CloudEvents `subject` (the aggregate id).
--   key         the Kafka message key (the aggregate id).
--   dataschema  the CloudEvents `dataschema` URN.
--   value       the structured-mode CloudEvents JSON bytes.
--   headers     [{"key":..,"value":..}] Kafka headers (content-type).
-- This context consumes nothing, so there is no processed_events (inbox) table.
CREATE TABLE outbox_events (
    id           BIGSERIAL   PRIMARY KEY,
    event_id     TEXT        NOT NULL,
    topic        TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    subject      TEXT        NOT NULL,
    key          BYTEA,
    dataschema   TEXT        NOT NULL,
    value        BYTEA       NOT NULL,
    headers      JSONB       NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    last_error   TEXT,
    CONSTRAINT outbox_events_event_id_topic_key UNIQUE (event_id, topic)
);

-- The relay only ever scans the unpublished tail; keep that scan tiny.
CREATE INDEX idx_outbox_events_unpublished ON outbox_events (id) WHERE published_at IS NULL;

-- idempotency_keys: the Idempotency-Key middleware of the REST commands. A
-- repeat of (route, key) with the same request_hash replays the stored
-- response; a different hash is a conflict. response_status/response_body are
-- NULL while the first request is still in flight. Rows live 24 hours: the
-- cleanup is DELETE ... WHERE created_at < now() - interval '24 hours'.
CREATE TABLE idempotency_keys (
    route           TEXT        NOT NULL,
    key             TEXT        NOT NULL,
    request_hash    TEXT        NOT NULL,
    response_status INTEGER,
    response_body   BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (route, key)
);

CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys (created_at);
