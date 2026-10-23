# ADR 0004: CloudEvents envelope and type catalogue

## Status

Accepted (2026-10-23).

## Context

Every Kafka message in the retail network is a CloudEvents 1.0 event in structured
mode (retail-network-docs `model/model.yaml` `conventions.cloudevents`; the repo
rule `.claude/rules/fleet/cloudevents.md`). This ADR is the catalogue of every
type node-topology publishes. `TestEventCatalogueMatchesContract` checks it
against `apis/asyncapi.yaml`. node-topology consumes no event (ADR 0003), so
there is no consumed list.

## Decision

### Envelope

- `specversion` `1.0`; `id` a UUID persisted with the outbox row (stable across
  relay retries; consumers dedupe on it); `source`
  `/retailnetwork/node-topology`; `subject` the aggregate root id (`nodeId`,
  `laneId`, `postalPrefix` or `regionId`); `time` the instant the change was
  committed, in UTC; `datacontenttype` `application/json`; `dataschema`
  `urn:retailnetwork:node-topology:events:<EventName>:v1`.
- Kafka value = the JSON event format; header
  `content-type: application/cloudevents+json; charset=UTF-8`; Kafka key = the
  subject, so all events of one aggregate stay ordered on one partition.
- `data` is camelCase; optional fields are omitted when unset (the exceptions
  are the three nullable `NodeRegistered` fields, which are always present);
  timestamps are RFC 3339 UTC.
- Built and decoded only through `internal/adapters/kafka/cloudevents`.
- `<entity>` is the raising aggregate, lowercase, no separators: `node`,
  `lane`, `servicearea`, `region`. Tier is `logistics`.

### Published types

Every type below goes to the topic `retailnetwork.node-topology.events`. The
model declares no analytics-only event for this context, so nothing is written
to `retailnetwork.node-topology.analytics`.

```text
type: com.retailnetwork.logistics.node-topology.node.NodeRegistered stream: events
type: com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared stream: events
type: com.retailnetwork.logistics.node-topology.node.NodeActivated stream: events
type: com.retailnetwork.logistics.node-topology.node.NodeSuspended stream: events
type: com.retailnetwork.logistics.node-topology.node.NodeRetired stream: events
type: com.retailnetwork.logistics.node-topology.lane.LaneOpened stream: events
type: com.retailnetwork.logistics.node-topology.lane.LaneClosed stream: events
type: com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned stream: events
type: com.retailnetwork.logistics.node-topology.region.RegionDefined stream: events
```

| Event | Subject / key | Raised when | `data` |
|---|---|---|---|
| `NodeRegistered` | `nodeId` | RegisterNode is saved (state PROPOSED) | `{nodeId, nodeType, subtype, operator, vendorId, vendorNodeCode, regionId, timezone, postalCode, countryCode}` |
| `NodeOperationalProfileDeclared` | `nodeId` | a new profile revision is saved | `{nodeId, profileRevision, timezone, operatingHoursByDay, holidays, handlingTimeMinutes, cutoffs, throughputCap, sortDwellMinutes?, declaredBy}` |
| `NodeActivated` | `nodeId` | ActivateNode is saved | `{nodeId, activatedAt}` |
| `NodeSuspended` | `nodeId` | SuspendNode is saved | `{nodeId, reason, suspendedAt}` |
| `NodeRetired` | `nodeId` | RetireNode is saved | `{nodeId, retiredAt}` |
| `LaneOpened` | `laneId` | OpenLane is saved | `{laneId, originNodeId, destinationNodeId, mode, nominalTransitMinutes}` |
| `LaneClosed` | `laneId` | CloseLane is saved | `{laneId, closedAt}` |
| `ServiceAreaAssigned` | `postalPrefix` | each saved assignment version | `{postalPrefix, deliveryStationNodeId, regionId, assignmentVersion}` |
| `RegionDefined` | `regionId` | DefineRegion is saved | `{regionId, code, name}` |

Payload rules:

- The `data` field lists are exactly the model's `publishes[].data` lists. Wire
  types and the nullability below are an implementation decision recorded in
  `apis/asyncapi.yaml` (see ADR 0002, assumption A9).
- `NodeRegistered` carries `subtype`, `vendorId` and `vendorNodeCode` as JSON
  `null` when they do not apply. Capabilities are not on the wire: consumers
  derive them from `nodeType` (policy DefaultCapabilitiesByType).
- `NodeOperationalProfileDeclared` omits `sortDwellMinutes` unless the node is a
  SORTATION_CENTER or an AIR_HUB, and a cutoff omits `days` (every operating
  day) and `maxOrders` when unset. A weekday missing from `operatingHoursByDay`
  is closed.
- `NodeSuspended` and `NodeRetired` do not announce the closing of lanes or the
  unassigning of service areas: neither happens (policy
  SuspendedEndpointsUnusable).
- A breaking payload change is a new `.v2` type and dataschema, never a
  mutation of v1.

## Consequences

- Consumers need nothing but this catalogue and `apis/asyncapi.yaml`; there is
  no shared Go code.
- Adding a published type requires a line here in the same commit as its
  asyncapi message, or the architecture fitness test fails.
