---
id: domain-events
title: Domain Events
sidebar_label: Domain Events
description: "The nine CloudEvents types Node Topology publishes, their payloads, producing commands and consumers; it consumes no event."
---

# Domain Events

Every event this context publishes, with its full CloudEvents type, and the
(empty) list of events it consumes. The envelope rules are the domain-wide
[event standard](https://iqvo.github.io/warehouse-docs/network/strategic-design/event-standard-cloudevents).

## Envelope

| Attribute | Value |
| --- | --- |
| `specversion` | `1.0` (structured JSON, mandatory on every Kafka message) |
| `id` | a UUID minted once when the outbox row is written; a relay retry re-sends the same `id` |
| `source` | `/retailnetwork/node-topology` |
| `type` | `com.retailnetwork.logistics.node-topology.<entity>.<EventName>`; tier `logistics`; entity `node`, `lane`, `servicearea` or `region` |
| `subject` | the aggregate root id: `nodeId`, `laneId`, `postalPrefix` or `regionId` |
| `time` | the instant the change was committed, UTC |
| `datacontenttype` | `application/json` |
| `dataschema` | `urn:retailnetwork:node-topology:events:<EventName>:v1` |
| Topic | `retailnetwork.node-topology.events` |
| Kafka key | the same aggregate root id as `subject`, so one aggregate's events stay in order on one partition |

Every event leaves through a transactional outbox, written in the same
transaction as the aggregate (`internal/adapters/outbound/postgres/outbox_repository.go`,
[ADR 0005](/docs/adr/0005-outbox-relay-and-attempt-cap)).

Source: `internal/adapters/kafka/cloudevents/cloudevents.go`,
`internal/adapters/outbound/kafka/encoder.go`, `payloads.go`,
`internal/domain/*/events.go` and `apis/asyncapi.yaml`.

## Published

### Summary

| Event | Full `type` | Subject / key | Producing command | Consumers |
| --- | --- | --- | --- | --- |
| `NodeRegistered` | `com.retailnetwork.logistics.node-topology.node.NodeRegistered` | `nodeId` | `RegisterNode` | `offer-availability`, `delivery-promise`, `order-sourcing`, `linehaul-transportation`, `last-mile-delivery`, `node-performance`, `vendor-connectivity`, `seller-inbound` |
| `NodeOperationalProfileDeclared` | `com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared` | `nodeId` | `DeclareOperationalProfile` | `delivery-promise`, `order-sourcing`, `linehaul-transportation`, `node-performance`, `vendor-connectivity`, `seller-inbound` |
| `NodeActivated` | `com.retailnetwork.logistics.node-topology.node.NodeActivated` | `nodeId` | `ActivateNode` | `offer-availability`, `delivery-promise`, `order-sourcing`, `linehaul-transportation`, `vendor-connectivity`, `seller-inbound` |
| `NodeSuspended` | `com.retailnetwork.logistics.node-topology.node.NodeSuspended` | `nodeId` | `SuspendNode` | `offer-availability`, `delivery-promise`, `order-sourcing`, `linehaul-transportation`, `vendor-connectivity`, `seller-inbound` |
| `NodeRetired` | `com.retailnetwork.logistics.node-topology.node.NodeRetired` | `nodeId` | `RetireNode` | `offer-availability`, `delivery-promise`, `order-sourcing`, `linehaul-transportation`, `vendor-connectivity`, `seller-inbound` |
| `LaneOpened` | `com.retailnetwork.logistics.node-topology.lane.LaneOpened` | `laneId` | `OpenLane` | `linehaul-transportation`, `delivery-promise`, `order-sourcing` |
| `LaneClosed` | `com.retailnetwork.logistics.node-topology.lane.LaneClosed` | `laneId` | `CloseLane` | `linehaul-transportation`, `delivery-promise`, `order-sourcing` |
| `ServiceAreaAssigned` | `com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned` | `postalPrefix` | `AssignServiceArea` | `delivery-promise`, `order-sourcing`, `last-mile-delivery` |
| `RegionDefined` | `com.retailnetwork.logistics.node-topology.region.RegionDefined` | `regionId` | `DefineRegion` | `delivery-promise`, `order-sourcing` |

The consumer column is the network model's subscriber list
(see [Integration](/docs/ecosystem/integration#subscribers-and-what-they-use));
this repository cannot see its consumers. The field lists below are the
payload structs in `internal/adapters/outbound/kafka/payloads.go`; the
[event catalogue](/docs/api-reference/events) is the consumer-facing summary.

### NodeRegistered

Producing use case: `RegisterNode`. What consumers do with it: build their
node read models (`NodeStatusView`, `NodeProfileView`, `StationView`,
`VendorNodeView`).

| Field | Type | Meaning |
| --- | --- | --- |
| `nodeId` | string | The network's id of the node |
| `nodeType` | enum | `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`, `RETURNS_CENTER`, `SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB` |
| `subtype` | enum, nullable | A `FULFILLMENT_CENTER` subtype; null otherwise |
| `operator` | enum | `NETWORK` or `VENDOR` |
| `vendorId` | string, nullable | Set when `operator` is `VENDOR` |
| `vendorNodeCode` | string, nullable | The vendor's own code; set when `operator` is `VENDOR` |
| `regionId` | string | The node's region |
| `timezone` | string | IANA time zone of the building |
| `postalCode` | string | Postal code of the building |
| `countryCode` | string | ISO 3166-1 alpha-2 |

Capabilities are not on the wire: consumers derive them from `nodeType`
(policy `DefaultCapabilitiesByType`, model `node_types`).

### NodeOperationalProfileDeclared

Producing use case: `DeclareOperationalProfile`. What consumers do with it:
`linehaul-transportation` (re)generates pickup departures per cutoff
(`PickupFromCutoffs`); `delivery-promise` and `order-sourcing` update
`NodeProfileView` (handling time, cutoffs, throughput cap); `node-performance`
records the declaration it will measure against.

| Field | Type | Meaning |
| --- | --- | --- |
| `nodeId` | string | The node |
| `profileRevision` | integer | Strictly increasing per node |
| `timezone` | string | IANA time zone the local times are in |
| `operatingHoursByDay` | object | `{monday … sunday: {start, end}}`, local times as `HH:mm`; a missing day is closed |
| `holidays` | array of ISO dates | Days the node is closed all day |
| `handlingTimeMinutes` | integer | Declared time from order release to a package ready for handover |
| `cutoffs` | array of `{localTime, days?, maxOrders?}` | `localTime` (`HH:mm`) is when packages are ready for handover; `days` lists the weekdays it applies to (default: every operating day); `maxOrders` optional |
| `throughputCap` | `{value, timeUnit, unit}` | Maximum orders per time unit; `value` is an integer above zero, `timeUnit` is `HOUR` or `DAY`, `unit` is `ORDER` |
| `sortDwellMinutes` | integer, optional | Declared sort dwell; present only for `SORTATION_CENTER` and `AIR_HUB` nodes |
| `inbound` | object, optional | Built ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)): `{maxCartonWeightG, maxCartonDimsMm{l,w,h}, acceptsPallets, requiresSscc}`, each member optional; present only when the revision declares limits. Not gated on the `INBOUND` capability: the code accepts limits on any node type. Additive: a consumer that ignores it is unaffected. |
| `declaredBy` | enum | `VENDOR` or `NETWORK_OPS` |

### NodeActivated

Producing use case: `ActivateNode`. Consumers mark the node sellable and
promisable; `offer-availability` restores listings it suppressed for the
node's unavailability.

| Field | Type | Meaning |
| --- | --- | --- |
| `nodeId` | string | The node |
| `activatedAt` | timestamp | When it became `ACTIVE` |

### NodeSuspended

Producing use case: `SuspendNode`. `offer-availability` suppresses the node's
listings; `linehaul-transportation` cancels future pickups.

| Field | Type | Meaning |
| --- | --- | --- |
| `nodeId` | string | The node |
| `reason` | string | Why the planner suspended it |
| `suspendedAt` | timestamp | When it became `SUSPENDED` |

### NodeRetired

Producing use case: `RetireNode`. Same reactions as `NodeSuspended`, for
good.

| Field | Type | Meaning |
| --- | --- | --- |
| `nodeId` | string | The node |
| `retiredAt` | timestamp | When it became `RETIRED` |

### LaneOpened

Producing use case: `OpenLane`. `linehaul-transportation` may schedule
departures on it; `delivery-promise` and `order-sourcing` add it to their
graph.

| Field | Type | Meaning |
| --- | --- | --- |
| `laneId` | string | The lane |
| `originNodeId` | string | Origin node |
| `destinationNodeId` | string | Destination node |
| `mode` | enum | `GROUND` or `AIR` |
| `nominalTransitMinutes` | integer | Nominal transit time |

### LaneClosed

Producing use case: `CloseLane`.

| Field | Type | Meaning |
| --- | --- | --- |
| `laneId` | string | The lane |
| `closedAt` | timestamp | When it closed |

### ServiceAreaAssigned

Producing use case: `AssignServiceArea`.

| Field | Type | Meaning |
| --- | --- | --- |
| `postalPrefix` | string | The postal prefix |
| `deliveryStationNodeId` | string | The `DELIVERY_STATION` that serves it |
| `regionId` | string | The station's region |
| `assignmentVersion` | integer | Strictly increasing per prefix; consumers keep the highest |

### RegionDefined

Producing use case: `DefineRegion`.

| Field | Type | Meaning |
| --- | --- | --- |
| `regionId` | string | The region |
| `code` | string | Unique short code |
| `name` | string | Display name |

### Example envelope

`NodeOperationalProfileDeclared` is the event most decisions downstream
depend on: pickup departures, promise cutoffs and the throughput cap all come
from it. One complete envelope, with illustrative values:

```json
{
  "specversion": "1.0",
  "id": "3f6c2a9e-8b1d-4c7a-9e55-0d2f6b7a1c44",
  "source": "/retailnetwork/node-topology",
  "type": "com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared",
  "subject": "node-0f3a91",
  "time": "2026-10-22T13:05:12Z",
  "datacontenttype": "application/json",
  "dataschema": "urn:retailnetwork:node-topology:events:NodeOperationalProfileDeclared:v1",
  "data": {
    "nodeId": "node-0f3a91",
    "profileRevision": 3,
    "timezone": "America/Sao_Paulo",
    "operatingHoursByDay": {
      "monday": {"start": "06:00", "end": "22:00"},
      "tuesday": {"start": "06:00", "end": "22:00"},
      "wednesday": {"start": "06:00", "end": "22:00"},
      "thursday": {"start": "06:00", "end": "22:00"},
      "friday": {"start": "06:00", "end": "22:00"},
      "saturday": {"start": "08:00", "end": "14:00"}
    },
    "holidays": ["2026-12-25"],
    "handlingTimeMinutes": 240,
    "cutoffs": [
      {"localTime": "11:00"},
      {"localTime": "18:00", "days": ["monday", "tuesday", "wednesday", "thursday", "friday"], "maxOrders": 1200}
    ],
    "throughputCap": {"value": 400, "timeUnit": "HOUR", "unit": "ORDER"},
    "declaredBy": "VENDOR"
  }
}
```

The values are illustrative. Sunday is absent, so the node is closed on
Sunday. The 11:00 cutoff has no `days`, so it applies to every operating day
(Monday to Saturday); the 18:00 cutoff names Monday to Friday because
Saturday closes at 14:00 (`ErrCutoffOutsideOperatingHours`). The example is a
fulfillment centre, so it carries no `sortDwellMinutes`
(`ErrSortDwellNotAllowed`).

## Consumed

None. The context has no Kafka consumer, no consumer group and no inbox
table to dedupe in ([ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads)).
Its inputs are the REST commands listed on [Use cases](/docs/ddd/use-cases).

## Analytics

Nothing is published on `retailnetwork.node-topology.analytics`: the only
topic the relay writes is `retailnetwork.node-topology.events`.
