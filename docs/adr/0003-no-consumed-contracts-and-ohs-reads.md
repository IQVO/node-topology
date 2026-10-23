# ADR 0003: no consumed contracts, Open Host Service reads, outbox publishing and versioned profile revisions

## Status

Accepted (2026-10-23).

## Context

node-topology sits at the root of the retail network's dependency graph: ten
contexts read its nodes, lanes, regions and service areas, and it depends on none
of them (model `consumes: []`, `sync_calls: []`). Writes are rare (tens per day,
about 10^3 nodes and 10^4 lanes and prefixes).

## Decision

1. **No consumed contracts.** The service has no Kafka consumer, no consumer
   group, no inbox table and no `*_CONSUMER_GROUP` variable. `DATABASE_URL` and
   `KAFKA_BROKERS` are its only mandatory settings. It calls no sibling context,
   over REST or otherwise, and there are no `sync_calls`.
2. **Open Host Service reads.** `GET /nodes/{nodeId}`, `GET /nodes`,
   `GET /lanes` and `GET /service-areas/{postalPrefix}` are the published
   language for every reader (`GetNode`, `ListNodes`, `ListLanes`,
   `ResolveServiceArea`). They read the aggregates' own tables; there is no
   separate read model (model `read_models: []`) because the load is low and each
   query is a lookup or one-table filter. The neighbours that sit on a hot path
   keep their own event-fed copies and never call this service per request.
   Read DTOs live in `internal/application/views`, not in the domain.
3. **Outbox publishing.** Every command writes the aggregate and one
   `outbox_events` row per raised event, with the CloudEvents envelope already
   encoded and its `id` minted once, in ONE Postgres transaction. An always-on
   relay drains the table to `retailnetwork.node-topology.events`
   (at-least-once; a retry republishes the same `id`). Nothing is published
   outside the outbox. The type catalogue is ADR 0004.
4. **Optimistic concurrency.** Each aggregate carries a `version` (ADR 0002). A
   save expects `stored version == aggregate version - 1`; a mismatch is
   `ErrConcurrentUpdate` (409), retried once by reloading.
5. **Versioned profile revisions as rows.** The Node row holds identity, type,
   operator, region, time zone, location, status and its timestamps. Each
   operational profile revision is its OWN row in `node_operational_profiles`,
   keyed `(node_id, profile_revision)`, never updated after insert: a new
   declaration inserts a row, and the node points at the current revision (the
   highest). `GET /nodes/{nodeId}` returns the node with its current profile;
   earlier revisions stay queryable for audit and for `node-performance`, which
   measures against what was declared at the time. The domain `Node` holds the
   current profile only; the repository inserts the row when the snapshot's
   profile revision is higher than the stored one.
6. **Cross-aggregate uniqueness in the database.** `(vendor_id, vendor_node_code)`
   unique on nodes, a partial unique index on `(origin, destination, mode)` where
   the lane is `OPEN`, and a unique `code` on regions. The repository maps a
   violation to the domain's `ErrVendorNodeCodeTaken`, `ErrDuplicateLane` or
   `ErrRegionCodeTaken` (ADR 0002, A10).
7. **Suspending closes nothing.** `SuspendNode` and `RetireNode` leave OPEN lanes
   and ASSIGNED service areas as they are (policy `SuspendedEndpointsUnusable`);
   consumers react to `NodeSuspended` and `NodeRetired`.

## Consequences

- The service has one outbox relay and zero consumers; the real-wiring test needs
  no Kafka producer on the test side.
- Readers get a stable REST surface plus events; they must keep their own
  copies for latency and availability.
- A profile revision can never be rewritten, so a published
  `NodeOperationalProfileDeclared` always matches a stored row.
