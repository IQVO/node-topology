---
id: events
title: Event catalogue
sidebar_label: Events
description: "The ten CloudEvents Node Topology publishes on retailnetwork.node-topology.events: types, keys, payload fields, delivery guarantees and consumer rules."
---

# Event catalogue

Node Topology publishes ten events and consumes none. The contract is `apis/asyncapi.yaml` (AsyncAPI 3.0.0); the types are catalogued in [ADR 0004](/docs/adr/0004-cloudevents-envelope-and-type-catalogue) and `TestEventCatalogueMatchesContract` fails the build if the two drift. Every field with its example is on [Domain events](/docs/ddd/domain-events); this page is the reference for consumers.

## Channel and envelope

| Item | Value |
| --- | --- |
| Topic | `retailnetwork.node-topology.events` (the only topic; nothing is published on an analytics topic) |
| Publisher | the outbox relay in `cmd/api`, `acks=all`, hash partitioner |
| Kafka key | the CloudEvents `subject`: `nodeId`, `laneId`, `postalPrefix` or `regionId`, so one aggregate's events stay ordered on one partition |
| Kafka header | `content-type: application/cloudevents+json; charset=UTF-8`, plus `traceparent` when the write had a span |
| Mode | CloudEvents 1.0, structured |
| Required attributes | `specversion` `1.0`, `id` (UUID), `source` `/retailnetwork/node-topology`, `type`, `subject`, `time` (RFC 3339 UTC), `datacontenttype` `application/json`, `dataschema` `urn:retailnetwork:node-topology:events:<EventName>:v1`, `data` |
| Type pattern | `com.retailnetwork.logistics.node-topology.<entity>.<EventName>`, entity `node`, `lane`, `servicearea` or `region` |
| `data` conventions | camelCase; timestamps RFC 3339 UTC; local times `HH:mm` in the declared `timezone`; optional fields omitted when unset except the three nullable `NodeRegistered` fields |

Sources: `internal/adapters/kafka/cloudevents/cloudevents.go`, `internal/adapters/outbound/kafka/encoder.go`, `payloads.go`.

## Types

| Event | Full `type` | Subject and key | Raised by | `data` fields |
| --- | --- | --- | --- | --- |
| `NodeRegistered` | `com.retailnetwork.logistics.node-topology.node.NodeRegistered` | `nodeId` | `RegisterNode` | `nodeId`, `nodeType`, `subtype` (null unless a fulfilment centre), `operator`, `vendorId` and `vendorNodeCode` (null for network nodes), `regionId`, `timezone`, `postalCode`, `countryCode` |
| `NodeOperationalProfileDeclared` | `com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared` | `nodeId` | `DeclareOperationalProfile` | `nodeId`, `profileRevision`, `timezone`, `operatingHoursByDay`, `holidays`, `handlingTimeMinutes`, `cutoffs[{localTime, days?, maxOrders?}]`, `throughputCap{value, timeUnit, unit}`, `sortDwellMinutes?`, `inbound?`, `declaredBy` |
| `NodeActivated` | `com.retailnetwork.logistics.node-topology.node.NodeActivated` | `nodeId` | `ActivateNode` | `nodeId`, `activatedAt` |
| `NodeSuspended` | `com.retailnetwork.logistics.node-topology.node.NodeSuspended` | `nodeId` | `SuspendNode` | `nodeId`, `reason`, `suspendedAt` |
| `NodeRetired` | `com.retailnetwork.logistics.node-topology.node.NodeRetired` | `nodeId` | `RetireNode` | `nodeId`, `retiredAt` |
| `LaneOpened` | `com.retailnetwork.logistics.node-topology.lane.LaneOpened` | `laneId` | `OpenLane` | `laneId`, `originNodeId`, `destinationNodeId`, `mode`, `nominalTransitMinutes` |
| `LaneClosed` | `com.retailnetwork.logistics.node-topology.lane.LaneClosed` | `laneId` | `CloseLane` | `laneId`, `closedAt` |
| `LaneCostDeclared` | `com.retailnetwork.logistics.node-topology.lane.LaneCostDeclared` | `laneId` | `DeclareLaneCost` | `laneId`, `costPerUnitCents`, `declaredAt` |
| `ServiceAreaAssigned` | `com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned` | `postalPrefix` | `AssignServiceArea` | `postalPrefix`, `deliveryStationNodeId`, `regionId`, `assignmentVersion` |
| `RegionDefined` | `com.retailnetwork.logistics.node-topology.region.RegionDefined` | `regionId` | `DefineRegion` | `regionId`, `code`, `name` |

### Field notes

- `nodeType` is one of `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`, `RETURNS_CENTER`, `SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB`; `subtype` one of `SORTABLE`, `NON_SORTABLE`, `XL`, `SOFT_LINES`, `SUB_SAME_DAY`, `FRESH`, `MICRO`; `operator` `NETWORK` or `VENDOR`; `mode` `GROUND` or `AIR`; `declaredBy` `VENDOR` or `NETWORK_OPS`; `throughputCap.timeUnit` `HOUR` or `DAY`, `unit` always `ORDER`.
- **Capabilities are not on the wire.** Derive them from `nodeType` (policy `DefaultCapabilitiesByType`).
- `profileRevision` strictly increases per node and `assignmentVersion` strictly increases per prefix: consumers keep the highest.
- In `operatingHoursByDay` a weekday that is missing is closed; `holidays` are ISO dates closed all day; a cutoff without `days` applies to every operating day; `sortDwellMinutes` is present only for sortation centres and air hubs.
- `inbound` is present only when the revision declares inbound limits; every limit inside it is optional: `maxCartonWeightG` (grams), `maxCartonDimsMm{l, w, h}` (millimetres), `acceptsPallets`, `requiresSscc`. Absent means unchecked. See [ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile).
- `NodeSuspended` and `NodeRetired` do not close the node's lanes or service areas (policy `SuspendedEndpointsUnusable`): consumers treat them as unusable while the node is not `ACTIVE`.
- `LaneClosed` is final: there is no reopen event; a new lane raises a new `LaneOpened`.
- `LaneCostDeclared` carries `costPerUnitCents`, integer cents from 0 to 10000000 (0 is a legal declaration); each declaration supersedes the previous value, so keep the latest per `laneId`. A lane never declared on publishes nothing. See [ADR 0014](/docs/adr/0014-lane-cost-declaration).

## Delivery guarantees and consumer rules

- **At least once.** The relay may republish a row after a crash; the `id` is minted once, at append, and stored with the row, so a duplicate carries the same `id`. Deduplicate on it.
- **Ordering.** Per aggregate, by key, while the relay is healthy. A row that reaches the attempt cap (`OUTBOX_MAX_ATTEMPTS`, default 20) is skipped and later rows overtake it until it is re-armed; this is why consumers keep the highest revision or version instead of trusting arrival order.
- **Dispatch on the full `type`; ignore unknown types.** A breaking payload change is a new `.v2` type, never a mutation of `v1`.
- **No replay API.** The topic is the history; republishing means re-arming an outbox row ([runbook](/docs/operations/runbook#the-outbox-relay)).

Who subscribes, and to which events, is on [Integration](/docs/ecosystem/integration).
