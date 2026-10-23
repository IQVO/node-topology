---
paths:
  - "internal/adapters/**/kafka/**"
  - "internal/adapters/outbound/outbox/**"
  - "apis/asyncapi*"
---

# Cross-service integration events (Kafka)

node-topology PUBLISHES nine integration events to
`retailnetwork.node-topology.events` and CONSUMES nothing (no consumer, no
consumer group, no inbox table, no `*_CONSUMER_GROUP` variable; ADR 0003). The
topic `retailnetwork.node-topology.analytics` is unused: no event is
analytics-only. Never produce or consume a `warehouse.*` topic.

## Events: CloudEvents 1.0 is MANDATORY

Every Kafka message this service produces is a CloudEvents 1.0 event in
structured content mode. This is a hard rule, not a preference:

- No flat envelope (`event_id`/`event_type`/`occurred_at`), no dual-write, no
  envelope toggle env var. `internal/architecture/fitness_test.go`'s
  `TestNoEventEnvelopeToggleOrFlatEnvelope` fails CI on any of them.
- Build and validate the envelope with `github.com/cloudevents/sdk-go/v2/event`
  through ONE helper package, `internal/adapters/kafka/cloudevents` (`New`,
  `Decode`, `ContentTypeHeader`). Transport stays `segmentio/kafka-go`; no
  hand-rolled CloudEvent structs. The only caller is the encoder
  `internal/adapters/outbound/kafka/encoder.go`.
- Every message carries the Kafka header
  `content-type: application/cloudevents+json; charset=UTF-8`. Message key =
  aggregate id, `kafkago.Hash{}` balancer (`internal/adapters/outbound/kafka/writer.go`).
- Attributes: `specversion=1.0`; `id` (UUID v4 minted ONCE per event at outbox
  append and stored in `outbox_events.event_id`, so a redelivery carries the same
  id); `source=/retailnetwork/node-topology`; `type`; `subject` (the aggregate
  id: nodeId, laneId, postalPrefix or regionId; never empty); `time` (the domain
  occurred-at, UTC); `datacontenttype=application/json`;
  `dataschema=urn:retailnetwork:node-topology:events:<EventName>:v1`. No custom
  extension attributes without an ADR.
- `type` = `com.retailnetwork.logistics.node-topology.<entity>.<EventName>` with
  entity `node`, `lane`, `servicearea` or `region` (lowercase, no separators).
  A breaking payload change is a new `.v2` dataschema, never a mutation of v1;
  evolution is additive only.
- Payloads follow the wire conventions of contracts-delta: ids are strings,
  instants RFC 3339 UTC, durations integers ending in `Minutes`, enums UPPER_SNAKE,
  optional fields omitted (the three nullable `NodeRegistered` fields are the
  documented exception, ADR 0004), arrays always present.
- Tests: a golden exact-JSON test per published type
  (`internal/adapters/outbound/kafka/encoder_test.go`), the catalogue-vs-contract
  sensor (`TestEventCatalogueMatchesContract`), and the Kafka proof against a real
  broker (`internal/adapters/outbound/outbox/relay_integration_test.go`), all via
  testcontainers.

### Published types

All on `retailnetwork.node-topology.events`; `subject` and Kafka key is the
aggregate id; `dataschema` is `urn:retailnetwork:node-topology:events:<Event>:v1`.

| `type` | `subject` |
| --- | --- |
| `com.retailnetwork.logistics.node-topology.node.NodeRegistered` | nodeId |
| `com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared` | nodeId |
| `com.retailnetwork.logistics.node-topology.node.NodeActivated` | nodeId |
| `com.retailnetwork.logistics.node-topology.node.NodeSuspended` | nodeId |
| `com.retailnetwork.logistics.node-topology.node.NodeRetired` | nodeId |
| `com.retailnetwork.logistics.node-topology.lane.LaneOpened` | laneId |
| `com.retailnetwork.logistics.node-topology.lane.LaneClosed` | laneId |
| `com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned` | postalPrefix |
| `com.retailnetwork.logistics.node-topology.region.RegionDefined` | regionId |

### Consumed types

None.

## Transactional outbox

A use case saves the aggregates and appends the encoded events in ONE Postgres
transaction (`ports.EventOutbox`, `internal/adapters/outbound/postgres/outbox_repository.go`).
The relay (`internal/adapters/outbound/outbox/relay.go`, always on, started by
`cmd/api/relay.go`) drains `outbox_events` in id order, at-least-once. The cap
`OUTBOX_MAX_ATTEMPTS` (default 20, 0 = unlimited) excludes a poison row; re-arm it
with `UPDATE outbox_events SET attempts = 0 WHERE id = ...` (ADR 0005). The first
write to a topic that does not exist yet can fail once with an unknown-topic
error: the relay retries it with the same id. The caller of `NewOutboxRelay` must
pass `WithMaxAttempts(...)` inline (`TestOutboxRelaysPassAttemptCap`).

## Consumer group id

This service consumes no Kafka topic, so it has no consumer group. If a consumer
is ever added, its group id must come from a mandatory env var (a boot error naming
it), never a string literal (`TestKafkaConsumerGroupNeverHardcodedInline`), and
the inbox table and dispatch rules of `.claude/rules/fleet/kafka-testing-and-consumers.md`
apply.
