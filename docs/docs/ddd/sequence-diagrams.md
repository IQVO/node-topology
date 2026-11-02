---
id: sequence-diagrams
title: Sequence Diagrams
sidebar_label: Sequence Diagrams
description: "UML sequence diagrams for every Node Topology command use case, from the HTTP adapter through the aggregate, repository and outbox to Kafka."
---

# Sequence Diagrams

One UML sequence diagram per command use case of the service. The participants
are the same in every diagram:

| Participant | Package |
| --- | --- |
| http adapter | `internal/adapters/inbound/http`, including the `Idempotency-Key` middleware |
| use case | `internal/application/usecases` |
| aggregate | `internal/domain/node`, `lane`, `servicearea` or `region` |
| Repositories | `internal/adapters/outbound/postgres` behind `internal/application/ports` |
| Postgres tx | the one transaction the middleware opens: the key row, the aggregate rows and `outbox_events` |
| relay to Kafka | `internal/adapters/outbound/outbox` and `outbound/kafka`, publishing to `retailnetwork.node-topology.events` |

Every state-changing route requires an `Idempotency-Key` header (`400
idempotency-key-required` without one). The middleware opens a transaction,
claims the key and runs the handler inside a savepoint of it, so the key,
the aggregate rows and the outbox rows of a 2xx commit together. The same key
with the same request replays the first answer (header `Idempotency-Replayed:
true`), the same key with another request is `409 idempotency-key-reused`. A 4xx
is stored too, a 5xx rolls everything back and is not stored. Each diagram shows
the optimistic-concurrency version check (or the unique index that backs a
create), the outbox write and the relay. When the version check fails the use
case runs the whole unit once more on fresh state and only then answers `409
concurrent-update`. A domain error writes nothing and publishes nothing. The
problem `type` slugs match [Use cases](/docs/ddd/use-cases). Path parameters are
written without braces in the diagrams.

## 1. RegisterNode

`POST /nodes`, published type `com.retailnetwork.logistics.node-topology.node.NodeRegistered`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Node
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Regions.Get regionId (absent is passed on as nil)
        UC->>A: Register type, subtype, operator, vendorRef, region
        alt bad value
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        else operator not allowed for the type
            A-->>UC: ErrOperatorNotAllowedForNodeType
            UC-->>H: 422 operator-not-allowed-for-node-type
        else region not defined
            A-->>UC: ErrUnknownRegion
            UC-->>H: 422 unknown-region
        else subtype not of the node type
            A-->>UC: ErrSubtypeNotAllowed
            UC-->>H: 422 subtype-not-allowed
        end
        Note over A: capabilities derived from the type, state PROPOSED, version 1
        opt vendor node
            UC->>R: Nodes.FindByVendorRef vendorId, vendorNodeCode
            alt code already used by this vendor
                R-->>UC: existing node
                UC-->>H: 409 vendor-node-code-taken
            end
        end
        UC->>R: Nodes.Save (insert)
        alt unique index nodes_vendor_ref_key hit
            R-->>UC: duplicate
            UC-->>H: 409 vendor-node-code-taken
        else saved
            UC->>TX: append outbox row NodeRegistered
            H->>TX: store response on the key row, commit
            H-->>H: 201 node
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish NodeRegistered
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/nodes.go`, `internal/domain/node/node.go`, `internal/adapters/outbound/postgres/outbox_drain.go`, `internal/adapters/outbound/outbox/relay.go`.

Omitted: a new node has no version to check, so the unique index on `(vendor_id, vendor_node_code)` takes that role and catches two concurrent registrations. A network node has no vendor reference and skips the lookup. The `Region` is read, never saved. Every registration attempt also increments the counter `topology.nodes.registered` with `outcome` and, on a rejection, `reason`.

## 2. DeclareOperationalProfile

`PUT /nodes/{nodeId}/operational-profile`, published type `com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Node
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Nodes.Get nodeId
        alt unknown node
            UC-->>H: 404 not-found
        end
        UC->>A: DeclareProfile declaration
        alt node RETIRED
            A-->>UC: ErrNodeRetired
            UC-->>H: 409 node-retired
        else revision not above current
            A-->>UC: ErrStaleProfileRevision
            UC-->>H: 409 stale-profile-revision
        else a value rule broken
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        else a cutoff on a day outside that day's hours
            A-->>UC: ErrCutoffOutsideOperatingHours
            UC-->>H: 422 cutoff-outside-operating-hours
        else ACTIVE FC revision without cutoff or handling time
            A-->>UC: ErrIncompleteOperationalProfile
            UC-->>H: 409 incomplete-operational-profile
        else sort dwell on a node that is not a sort centre or air hub
            A-->>UC: ErrSortDwellNotAllowed
            UC-->>H: 422 sort-dwell-not-allowed
        end
        Note over A: new revision becomes current, old ones untouched
        UC->>R: Nodes.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            R->>TX: insert row in node_profile_revisions
            UC->>TX: append outbox row NodeOperationalProfileDeclared
            H->>TX: store response on the key row, commit
            H-->>H: 200 node and profile
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish NodeOperationalProfileDeclared
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/nodes.go`, `internal/domain/node/node.go`, `internal/domain/node/profile.go`, `internal/adapters/outbound/postgres/node_repository.go`.

Omitted: a node with no profile yet takes any first revision above zero. A `NETWORK` node refuses `declaredBy` `VENDOR` (`validation-failed`). Both `vendor-connectivity` (`declaredBy` `VENDOR`) and the network-ops planner (`declaredBy` `NETWORK_OPS`) call this route.

## 3. ActivateNode

`POST /nodes/{nodeId}/activations`, published type `com.retailnetwork.logistics.node-topology.node.NodeActivated`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Node
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Nodes.Get nodeId
        UC->>A: Activate now
        alt node RETIRED
            A-->>UC: ErrNodeRetired
            UC-->>H: 409 node-retired
        else already ACTIVE
            A-->>UC: ErrIllegalTransition
            UC-->>H: 409 illegal-transition
        else FULFILLMENT_CENTER without cutoff or handling time
            A-->>UC: ErrIncompleteOperationalProfile
            UC-->>H: 409 incomplete-operational-profile
        end
        Note over A: PROPOSED or SUSPENDED to ACTIVE
        UC->>R: Nodes.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            UC->>TX: append outbox row NodeActivated
            H->>TX: store response on the key row, commit
            H-->>H: 200 node
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish NodeActivated
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/nodes.go`, `internal/domain/node/node.go`, `internal/adapters/outbound/postgres/outbox_drain.go`.

Omitted: the 404 for an unknown `nodeId` (same as diagram 2).

## 4. SuspendNode

`POST /nodes/{nodeId}/suspensions`, published type `com.retailnetwork.logistics.node-topology.node.NodeSuspended`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Node
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute reason
        UC->>R: Nodes.Get nodeId
        UC->>A: Suspend reason, now
        alt node RETIRED
            A-->>UC: ErrNodeRetired
            UC-->>H: 409 node-retired
        else not ACTIVE
            A-->>UC: ErrIllegalTransition
            UC-->>H: 409 illegal-transition
        else reason blank or longer than 500 characters
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        end
        Note over A: ACTIVE to SUSPENDED
        UC->>R: Nodes.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            UC->>TX: append outbox row NodeSuspended
            H->>TX: store response on the key row, commit
            H-->>H: 200 node
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish NodeSuspended
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/nodes.go`, `internal/domain/node/node.go`, `internal/adapters/outbound/postgres/outbox_drain.go`.

Omitted: the 404 for an unknown `nodeId`; the downstream reactions (on [Domain Message Flow](/docs/ddd/domain-message-flow)).

## 5. RetireNode

`POST /nodes/{nodeId}/retirements`, published type `com.retailnetwork.logistics.node-topology.node.NodeRetired`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Node
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Nodes.Get nodeId
        UC->>A: Retire now
        alt node already RETIRED
            A-->>UC: ErrNodeRetired
            UC-->>H: 409 node-retired
        else still PROPOSED
            A-->>UC: ErrIllegalTransition
            UC-->>H: 409 illegal-transition
        end
        Note over A: ACTIVE or SUSPENDED to RETIRED, terminal
        UC->>R: Nodes.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            UC->>TX: append outbox row NodeRetired
            H->>TX: store response on the key row, commit
            H-->>H: 200 node
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish NodeRetired
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/nodes.go`, `internal/domain/node/node.go`, `internal/adapters/outbound/postgres/outbox_drain.go`.

Omitted: the 404 for an unknown `nodeId`. The node's lanes and service areas are not touched (policy `SuspendedEndpointsUnusable`).

## 6. OpenLane

`POST /lanes`, published type `com.retailnetwork.logistics.node-topology.lane.LaneOpened`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Lane
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Nodes.Get origin, then Nodes.Get destination
        alt unknown node
            UC-->>H: 404 not-found
        end
        UC->>A: Open laneId, mode, transit, origin, destination
        alt bad mode or transit
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        else origin equals destination
            A-->>UC: ErrLaneLoop
            UC-->>H: 422 lane-loop
        else type pair not allowed
            A-->>UC: ErrLaneTypePairNotAllowed
            UC-->>H: 422 lane-type-pair-not-allowed
        else an end not ACTIVE
            A-->>UC: ErrLaneEndpointNotActive
            UC-->>H: 409 lane-endpoint-not-active
        end
        Note over A: state OPEN, version 1
        UC->>R: Lanes.FindOpen origin, destination, mode
        alt an OPEN lane exists
            UC-->>H: 409 duplicate-lane
        end
        UC->>R: Lanes.Save (insert)
        alt partial unique index lanes_one_open_key hit
            R-->>UC: duplicate
            UC-->>H: 409 duplicate-lane
        else saved
            UC->>TX: append outbox row LaneOpened
            H->>TX: store response on the key row, commit
            H-->>H: 201 lane
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish LaneOpened
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/lanes.go`, `internal/domain/lane/lane.go`, `internal/adapters/outbound/postgres/lane_repository.go`.

Omitted: the two `Node` aggregates are read, never saved.

## 7. CloseLane

`POST /lanes/{laneId}/closures`, published type `com.retailnetwork.logistics.node-topology.lane.LaneClosed`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Lane
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Lanes.Get laneId
        alt unknown lane
            UC-->>H: 404 not-found
        end
        UC->>A: Close now
        alt already CLOSED
            A-->>UC: ErrIllegalTransition
            UC-->>H: 409 illegal-transition
        end
        Note over A: OPEN to CLOSED, no ACTIVE endpoint needed
        UC->>R: Lanes.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            UC->>TX: append outbox row LaneClosed
            H->>TX: store response on the key row, commit
            H-->>H: 200 lane
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish LaneClosed
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/lanes.go`, `internal/domain/lane/lane.go`, `internal/adapters/outbound/postgres/lane_repository.go`.

Omitted: nothing else; `CloseLane` touches no other aggregate.

## 8. AssignServiceArea

`PUT /service-areas/{postalPrefix}`, published type `com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as ServiceArea
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>R: Nodes.Get station, then Regions.Get its region
        alt unknown station
            UC-->>H: 404 not-found
        end
        UC->>R: ServiceAreas.Get prefix
        Note over UC: absent means Assign, present means Reassign
        UC->>A: Assign or Reassign version, station, region
        alt bad prefix or version
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        else not an ACTIVE DELIVERY_STATION
            A-->>UC: ErrNotADeliveryStation
            UC-->>H: 409 not-a-delivery-station
        else version not above current
            A-->>UC: ErrStaleAssignmentVersion
            UC-->>H: 409 stale-assignment-version
        else station's region not defined
            A-->>UC: ErrUnknownRegion
            UC-->>H: 422 unknown-region
        end
        Note over A: ASSIGNED to ASSIGNED, regionId taken from the station
        UC->>R: ServiceAreas.Save with version check
        alt version changed since load
            R-->>UC: ErrConcurrentUpdate
            Note over UC: run the unit once more, then 409 concurrent-update
        else saved
            UC->>TX: append outbox row ServiceAreaAssigned
            H->>TX: store response on the key row, commit
            H-->>H: 200 service area
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish ServiceAreaAssigned
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/servicearea.go`, `internal/domain/servicearea/servicearea.go`, `internal/adapters/outbound/postgres/service_area_repository.go`.

Omitted: the station `Node` and its `Region` are read, never saved. The first assignment of a prefix is a create (no version to check), later ones update the single `service_areas` row.

## 9. DefineRegion

`POST /regions`, published type `com.retailnetwork.logistics.node-topology.region.RegionDefined`.

```mermaid
%%{init: {"sequence": {"wrap": true, "width": 120, "actorMargin": 24, "messageMargin": 28, "boxMargin": 6, "noteMargin": 6, "mirrorActors": false}}}%%
sequenceDiagram
    autonumber
    participant H as http adapter
    participant UC as use case
    participant A as Region
    participant R as Repositories
    participant TX as Postgres tx<br/>outbox + idem keys
    participant K as relay to Kafka
    H->>TX: claim Idempotency-Key, INSERT ON CONFLICT DO NOTHING
    alt key already stored
        TX-->>H: stored row
        H-->>H: same request replays the first response, another request is 422
    else new key
        H->>UC: Execute
        UC->>A: Define code, name
        alt bad code or name
            A-->>UC: ErrValidation
            UC-->>H: 422 validation-failed
        end
        UC->>R: Regions.FindByCode code
        alt code taken
            UC-->>H: 409 region-code-taken
        end
        UC->>R: Regions.Save (insert)
        alt unique constraint regions_code_key hit
            R-->>UC: duplicate
            UC-->>H: 409 region-code-taken
        else saved
            UC->>TX: append outbox row RegionDefined
            H->>TX: store response on the key row, commit
            H-->>H: 201 region
        end
    end
    K->>TX: claim unpublished rows
    K->>K: publish RegionDefined
    K->>TX: mark published
```

Source: `internal/adapters/inbound/http/idempotency.go`, `internal/application/usecases/regions.go`, `internal/domain/region/region.go`, `internal/adapters/outbound/postgres/region_repository.go`.

Omitted: nothing else.
