---
id: use-cases
title: Use Cases
sidebar_label: Use Cases
description: "Every application use case of Node Topology: nine commands and six reads, with trigger, inputs, invariants, errors and the events they raise."
---

# Use Cases

One use case per operation of `apis/openapi.yaml`, in `internal/application/usecases`. Nine commands change state and each raises exactly one event; six reads (`GetNode`, `ListNodes`, `ListLanes`, `ResolveServiceArea`, `GetRegion`, `ListRegions`) change nothing. No use case is driven by Kafka: the context consumes no event. Payloads are on the [event catalogue](/docs/api-reference/events), the step-by-step flows on [Sequence diagrams](/docs/ddd/sequence-diagrams).

## How every command commits

Source: `internal/application/usecases/writer.go`, `internal/adapters/inbound/http/idempotency.go`.

1. The REST adapter requires an `Idempotency-Key` (1 to 128 printable ASCII characters). A key already seen with the same method, path and body replays the stored response (header `Idempotency-Replayed: true`); the same key with another request is `409 idempotency-key-reused`. Keys live 24 hours in `idempotency_keys`.
2. Inside one `UnitOfWork` the use case loads the aggregate (and, where an invariant needs it, the other aggregate it reads), calls the domain method and saves the aggregate with an optimistic-concurrency version check.
3. In the same Postgres transaction `EventOutbox.Append` writes one already-encoded `outbox_events` row per event, and the idempotency record is stored.
4. The outbox relay publishes the row to `retailnetwork.node-topology.events` after commit.

A `ErrConcurrentUpdate` at step 2 reruns the whole attempt once on fresh state (`transact`); a second conflict answers `409 concurrent-update`. A domain error writes nothing and publishes nothing. A 4xx is stored and replayed; a 5xx rolls everything back and is not stored.

| Error (every use case) | HTTP | Problem slug |
| --- | --- | --- |
| `shared.ErrValidation` | 422 | `validation-failed` |
| `ports.ErrNotFound` | 404 | `not-found` |
| `shared.ErrIllegalTransition` | 409 | `illegal-transition` |
| `ports.ErrConcurrentUpdate` | 409 | `concurrent-update` |

## Triggers at a glance

| Use case | Trigger | Inbound | Event on success |
| --- | --- | --- | --- |
| `RegisterNode` | `vendor-connectivity` (vendor node) or a network-ops planner (parcel node) | `POST /nodes` | `NodeRegistered` |
| `DeclareOperationalProfile` | `vendor-connectivity` (`declaredBy` `VENDOR`) or a planner (`NETWORK_OPS`) | `PUT /nodes/{nodeId}/operational-profile` | `NodeOperationalProfileDeclared` |
| `ActivateNode` | planner | `POST /nodes/{nodeId}/activations` | `NodeActivated` |
| `SuspendNode` | planner | `POST /nodes/{nodeId}/suspensions` | `NodeSuspended` |
| `RetireNode` | planner | `POST /nodes/{nodeId}/retirements` | `NodeRetired` |
| `OpenLane` | planner | `POST /lanes` | `LaneOpened` |
| `CloseLane` | planner | `POST /lanes/{laneId}/closures` | `LaneClosed` |
| `AssignServiceArea` | planner, in bulk at network redesign | `PUT /service-areas/{postalPrefix}` | `ServiceAreaAssigned` |
| `DefineRegion` | planner | `POST /regions` | `RegionDefined` |

## RegisterNode

Source: `usecases/nodes.go`, `domain/node/node.go` (`Register`).

- **Input**: `nodeType`, `subtype` (fulfilment centres only), `operator`, `vendorRef{vendorId, vendorNodeCode}` (vendor nodes), `regionId`, `timezone`, `location{postalCode, countryCode, lat, lon}`.
- **Flow**: mint the node id; load the region (a miss reaches the domain as nil); validate the enumerations and the operator against the type table; validate ids, vendor reference, IANA-shaped timezone and location; require the region; check the subtype; pre-check the free `(vendorId, vendorNodeCode)`; save in `PROPOSED` at version 1; append `NodeRegistered`; answer `201`.
- **Invariants**: fulfilment centres, inbound cross-docks and returns centres are `VENDOR`-operated and carry a vendor reference; sortation centres, delivery stations and air hubs are `NETWORK`-operated and carry none; only a fulfilment centre has a subtype (`SORTABLE`, `NON_SORTABLE`, `XL`, `SOFT_LINES`, `SUB_SAME_DAY`, `FRESH`, `MICRO`); the vendor code is unique per vendor; `countryCode` is two uppercase letters; `lat` and `lon` come together; capabilities derive from the type.
- **Errors**: `operator-not-allowed-for-node-type` (422), `unknown-region` (422), `subtype-not-allowed` (422), `vendor-node-code-taken` (409), `validation-failed` (422).
- **Event**: `NodeRegistered`. Every attempt also increments `topology.nodes.registered` with the outcome and, on a refusal, the slug as `reason`.
- **Idempotency**: `vendor-connectivity` is expected to send its transaction id as the key, so a re-processed submission replays the first answer instead of failing on `vendor-node-code-taken`.

## DeclareOperationalProfile

Source: `usecases/nodes.go`, `domain/node/profile.go`, `profile_validate.go`, `inbound.go`.

- **Input**: `profileRevision`, `timezone`, `operatingHoursByDay`, `holidays[]`, `handlingTimeMinutes`, `cutoffs[{localTime, days?, maxOrders?}]`, `throughputCap{value, timeUnit, unit}`, `sortDwellMinutes` (sortation centres and air hubs), optional `inbound` limits ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)), `declaredBy`.
- **Flow**: load the node; refuse a retired node; refuse a revision not above the current one; validate the shape; check every cutoff against the operating hours of each of its days; check the profile fits the node; append the revision as a new immutable row; append `NodeOperationalProfileDeclared`; answer `200` with the node and its current profile. The node's lifecycle state does not change.
- **Invariants**: the revision strictly increases; hours open before they close (`HH:mm`, a missing weekday is closed); holidays are ISO dates, sorted and unique; `throughputCap.timeUnit` is `HOUR` or `DAY` and `unit` is `ORDER`; a cutoff lies inside the operating hours of every day it applies to (no `days` means every operating day) and two cutoffs may not share a time on a shared day; `sortDwellMinutes` only on sortation centres and air hubs; a `NETWORK` node's profile cannot be declared `VENDOR`; an ACTIVE fulfilment centre keeps at least one cutoff and a handling time above zero; inbound limits declare at least one limit, each positive.
- **Errors**: `stale-profile-revision` (409), `cutoff-outside-operating-hours` (422), `sort-dwell-not-allowed` (422), `incomplete-operational-profile` (409), `node-retired` (409), `validation-failed` (422).
- **Event**: `NodeOperationalProfileDeclared`.

## ActivateNode

Source: `domain/node/node.go` (`Activate`).

- **Flow**: load; guard the transition; a fulfilment centre needs a complete current profile; move to `ACTIVE`, set `activatedAt`, clear any suspension; append `NodeActivated`; answer `200`.
- **Preconditions**: the node is `PROPOSED` or `SUSPENDED`.
- **Errors**: `incomplete-operational-profile` (409), `node-retired` (409), `illegal-transition` (409, already `ACTIVE`).
- **Event**: `NodeActivated`. A node takes customer demand only while `ACTIVE` and with the `OUTBOUND` capability (`AcceptsDemand`).

## SuspendNode

- **Input**: `reason`, a trimmed non-blank string of at most 500 characters.
- **Preconditions**: the node is `ACTIVE`.
- **Errors**: `node-retired` (409), `illegal-transition` (409, `PROPOSED` or already `SUSPENDED`), `validation-failed` (422, bad reason).
- **Event**: `NodeSuspended`. The node's open lanes and assigned service areas are not touched (policy `SuspendedEndpointsUnusable`): consumers treat them as unusable while the node is not `ACTIVE`.

## RetireNode

- **Preconditions**: the node is `ACTIVE` or `SUSPENDED`. `RETIRED` is terminal.
- **Errors**: `node-retired` (409), `illegal-transition` (409, still `PROPOSED`).
- **Event**: `NodeRetired`. Lanes and service areas of the node are left as they are.

## OpenLane

Source: `usecases/lanes.go`, `domain/lane/lane.go`.

- **Input**: `originNodeId`, `destinationNodeId`, `mode` (`GROUND` or `AIR`), `nominalTransitMinutes` (1 to 2147483647).
- **Flow**: load both nodes (an unknown one is `404`); validate id, mode and transit; refuse a loop; check the type pair; require both ends `ACTIVE`; pre-check that no `OPEN` lane has the same origin, destination and mode; save `OPEN` at version 1 (a partial unique index guards a concurrent open); append `LaneOpened`; answer `201`.
- **Allowed pairs (origin to destination)**: fulfilment centre to sortation centre, delivery station or air hub; sortation centre to sortation centre, delivery station or air hub; air hub to air hub, sortation centre or delivery station; inbound cross-dock to fulfilment centre.
- **Errors**: `lane-loop` (422), `lane-type-pair-not-allowed` (422), `lane-endpoint-not-active` (409), `duplicate-lane` (409), `validation-failed` (422).
- **Event**: `LaneOpened`.

## CloseLane

- **Preconditions**: the lane is `OPEN`. Closing needs no active endpoint. There is no reopen: a new lane is opened instead.
- **Errors**: `illegal-transition` (409, already `CLOSED`), `not-found` (404).
- **Event**: `LaneClosed`.

## AssignServiceArea

Source: `usecases/servicearea.go`, `domain/servicearea/servicearea.go`.

- **Input**: path `postalPrefix` (1 to 16 characters from `A-Z`, `0-9`, `-`, matched exactly and never normalised), body `deliveryStationNodeId`, `assignmentVersion`.
- **Flow**: load the station (unknown is `404`) and its region; load the current assignment of the prefix, or start a new one; check the station is an `ACTIVE` delivery station; require a version above the current one; require the station's region to exist; assign and append `ServiceAreaAssigned`; answer `200`. The same operation covers first assignment and reassignment; there is no unassign.
- **Errors**: `not-a-delivery-station` (409), `stale-assignment-version` (409), `unknown-region` (422), `validation-failed` (422).
- **Event**: `ServiceAreaAssigned`, with the station's `regionId`.

## DefineRegion

Source: `usecases/regions.go`, `domain/region/region.go`.

- **Input**: `code` (1 to 16 characters from `A-Z`, `0-9`, `_`, `-`), `name` (non-blank, trimmed, at most 120 characters).
- **Flow**: mint the id; create the region at version 1; a unique index on `code` rejects a duplicate; append `RegionDefined`; answer `201`. A region is never renamed or removed.
- **Errors**: `region-code-taken` (409), `validation-failed` (422).
- **Event**: `RegionDefined`.

## Reads

Source: `usecases/queries.go`. Reads load through the repositories and return `views`; they raise no event and need no `Idempotency-Key`.

| Read | Route, tool or skill | Behaviour |
| --- | --- | --- |
| `GetNode` | `GET /nodes/{nodeId}`, MCP `get_node` | The node with its current profile; unknown id is `not-found`. |
| `ListNodes` | `GET /nodes?type=&region=&status=`, MCP `list_nodes`, A2A `find_nodes` | By id ascending; filters combine with AND; no match is an empty list; an unknown `type` or `status` is `validation-failed`. |
| `ListLanes` | `GET /lanes?origin=&destination=`, MCP `list_lanes` | Open and closed lanes, by id ascending. |
| `ResolveServiceArea` | `GET /service-areas/{postalPrefix}`, MCP `resolve_service_area`, A2A `serving_station` | The current assignment; an unassigned prefix is `not-found`. |
| `GetRegion`, `ListRegions` | `GET /regions/{regionId}`, `GET /regions`, MCP `get_region`, `list_regions` | Regions by id ascending, keyset-paginated. |
