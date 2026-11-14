---
id: eventstorming
title: EventStorming
sidebar_label: EventStorming
description: "Process-level EventStorming boards for Node Topology, covering node registration and activation, suspension and retirement, and lanes, regions and service areas. No hotspot is open."
---

# EventStorming

Process-level boards in the ddd-crew
[EventStorming glossary & cheat sheet](https://github.com/ddd-crew/eventstorming-glossary-cheat-sheet)
notation. Every sticky comes from the code: the commands are the use cases in
`internal/application/usecases`, the events those in `internal/domain/*/events.go`;
the neighbours' policies and read models come from the network model. A hotspot
would be an open question or a risk; none is open for this context today (see
the end of the page). The domain-wide board is
[EventStorming big picture](https://iqvo.github.io/warehouse-docs/network/strategic-design/eventstorming-big-picture).

## Legend

| Colour | Sticky | Meaning here |
| --- | --- | --- |
| Blue `#a5d8ff` | Command | A command use case |
| Yellow `#ffec99` | Aggregate | `Node`, `Lane`, `ServiceArea`, `Region` |
| Orange `#ffc078` | Domain Event | One of the ten published events |
| Lilac `#d0bfff` | Policy | This context's policy or a neighbour's reaction |
| Green `#b2f2bb` | Read Model | A neighbour's event-fed read model |
| Pink `#fcc2d7` | External System | Another bounded context |
| Red `#ff8787` | Hotspot | An open question or risk |
| Pale yellow `#fff9db` | Actor | A person (the network-ops planner) |

## 1. A node is registered, declared and activated

```mermaid
flowchart LR
    classDef command fill:#a5d8ff,stroke:#1971c2,color:#000
    classDef aggregate fill:#ffec99,stroke:#e67700,color:#000
    classDef event fill:#ffc078,stroke:#d9480f,color:#000
    classDef policy fill:#d0bfff,stroke:#6741d9,color:#000
    classDef readmodel fill:#b2f2bb,stroke:#2f9e44,color:#000
    classDef external fill:#fcc2d7,stroke:#c2255c,color:#000
    classDef hotspot fill:#ff8787,stroke:#c92a2a,color:#000
    classDef actor fill:#fff9db,stroke:#f08c00,color:#000
    VC["vendor-connectivity"]:::external
    PL["network-ops<br/>planner"]:::actor
    C1["RegisterNode"]:::command
    P1["DefaultCapabilities<br/>ByType"]:::policy
    G1["Node"]:::aggregate
    E1["NodeRegistered"]:::event
    C2["DeclareOperational<br/>Profile"]:::command
    E2["NodeOperational<br/>ProfileDeclared"]:::event
    C3["ActivateNode"]:::command
    E3["NodeActivated"]:::event

    VC --> C1
    PL --> C1
    C1 --> P1
    P1 --> G1
    G1 --> E1
    VC --> C2
    PL --> C2
    C2 --> G1
    G1 --> E2
    PL --> C3
    C3 --> G1
    G1 --> E3
```

Source: `internal/domain/node/events.go`, `internal/domain/lane/events.go`, `internal/domain/servicearea/events.go`, `internal/domain/region/events.go`, `internal/application/usecases/*.go`.

Omitted: the refusals (`ErrOperatorNotAllowedForNodeType`,
`ErrVendorNodeCodeTaken`, `ErrStaleProfileRevision`,
`ErrCutoffOutsideOperatingHours`, `ErrIncompleteOperationalProfile`,
`region.ErrUnknownRegion`, `ErrSubtypeNotAllowed`, `ErrSortDwellNotAllowed`,
`ErrInboundLimitsNotAllowed`), which
raise no event; the downstream reactions, on board 2.

## 2. What the network does with node facts

```mermaid
flowchart LR
    classDef command fill:#a5d8ff,stroke:#1971c2,color:#000
    classDef aggregate fill:#ffec99,stroke:#e67700,color:#000
    classDef event fill:#ffc078,stroke:#d9480f,color:#000
    classDef policy fill:#d0bfff,stroke:#6741d9,color:#000
    classDef readmodel fill:#b2f2bb,stroke:#2f9e44,color:#000
    classDef external fill:#fcc2d7,stroke:#c2255c,color:#000
    classDef hotspot fill:#ff8787,stroke:#c92a2a,color:#000
    classDef actor fill:#fff9db,stroke:#f08c00,color:#000
    E2["NodeOperational<br/>ProfileDeclared"]:::event
    E3["NodeActivated"]:::event
    E4["NodeSuspended"]:::event
    E5["NodeRetired"]:::event
    P2["PickupFromCutoffs<br/>linehaul"]:::policy
    P3["SuppressOnNode<br/>Unavailable<br/>offer-availability"]:::policy
    R1["NodeProfileView<br/>delivery-promise,<br/>order-sourcing"]:::readmodel
    R2["NodeStatusView<br/>offer-availability"]:::readmodel
    R3["VendorNodeView<br/>vendor-connectivity"]:::readmodel
    LH["linehaul-<br/>transportation"]:::external
    OA["offer-availability"]:::external

    E2 --> P2
    E4 --> P2
    E5 --> P2
    P2 --> LH
    E3 --> R2
    E4 --> P3
    E5 --> P3
    E3 --> P3
    P3 --> OA
    E2 --> R1
    E3 --> R1
    E4 --> R1
    E5 --> R1
    E3 --> R3
    E4 --> R3
    E5 --> R3
```

Source: `internal/domain/node/events.go`, `internal/domain/lane/events.go`, `internal/domain/servicearea/events.go`, `internal/domain/region/events.go`, `internal/application/usecases/*.go`.

Omitted: `NodeRegistered` feeding the same read models (and `StationView` in
`last-mile-delivery`); `node-performance`, which consumes `NodeRegistered` and
`NodeOperationalProfileDeclared` without effect (its handlers write only the inbox row);
`seller-inbound`, which keeps its `NodeInboundView` from the five node events.

## 3. Suspension and retirement

```mermaid
flowchart LR
    classDef command fill:#a5d8ff,stroke:#1971c2,color:#000
    classDef aggregate fill:#ffec99,stroke:#e67700,color:#000
    classDef event fill:#ffc078,stroke:#d9480f,color:#000
    classDef policy fill:#d0bfff,stroke:#6741d9,color:#000
    classDef readmodel fill:#b2f2bb,stroke:#2f9e44,color:#000
    classDef external fill:#fcc2d7,stroke:#c2255c,color:#000
    classDef hotspot fill:#ff8787,stroke:#c92a2a,color:#000
    classDef actor fill:#fff9db,stroke:#f08c00,color:#000
    PL["network-ops<br/>planner"]:::actor
    C4["SuspendNode"]:::command
    C5["RetireNode"]:::command
    C3["ActivateNode"]:::command
    G1["Node"]:::aggregate
    E4["NodeSuspended"]:::event
    E5["NodeRetired"]:::event
    E3["NodeActivated"]:::event
    P4["SuspendedEndpoints<br/>Unusable<br/>closes nothing"]:::policy

    PL --> C4
    PL --> C5
    PL --> C3
    C4 --> G1
    C5 --> G1
    C3 --> G1
    G1 --> E4
    G1 --> E5
    G1 --> E3
    E4 --> P4
    E5 --> P4
```

Source: `internal/domain/node/events.go`, `internal/domain/lane/events.go`, `internal/domain/servicearea/events.go`, `internal/domain/region/events.go`, `internal/application/usecases/*.go`.

The policy `SuspendedEndpointsUnusable` is this context's: the node's `OPEN`
lanes and `ASSIGNED` service areas stay as they are, and consumers treat them
as unusable while the node is not `ACTIVE`.

Omitted: `ErrNodeRetired` (any command on a `RETIRED` node) and
`ErrIllegalTransition` (any other command outside the transition table); the reasons a
planner suspends (free text of at most 500 characters; `node-performance` never
suspends a node).

## 4. Lanes, regions and service areas

```mermaid
flowchart LR
    classDef command fill:#a5d8ff,stroke:#1971c2,color:#000
    classDef aggregate fill:#ffec99,stroke:#e67700,color:#000
    classDef event fill:#ffc078,stroke:#d9480f,color:#000
    classDef policy fill:#d0bfff,stroke:#6741d9,color:#000
    classDef readmodel fill:#b2f2bb,stroke:#2f9e44,color:#000
    classDef external fill:#fcc2d7,stroke:#c2255c,color:#000
    classDef hotspot fill:#ff8787,stroke:#c92a2a,color:#000
    classDef actor fill:#fff9db,stroke:#f08c00,color:#000
    PL["network-ops<br/>planner"]:::actor
    C6["DefineRegion"]:::command
    G4["Region"]:::aggregate
    E8["RegionDefined"]:::event
    C7["OpenLane"]:::command
    C8["CloseLane"]:::command
    G2["Lane"]:::aggregate
    E6["LaneOpened"]:::event
    E7["LaneClosed"]:::event
    C10["DeclareLane<br/>Cost"]:::command
    E10["LaneCost<br/>Declared"]:::event
    C9["AssignService<br/>Area"]:::command
    G3["ServiceArea"]:::aggregate
    E9["ServiceArea<br/>Assigned"]:::event
    N1["Node, read for<br/>ACTIVE and type"]:::aggregate
    R4["LaneView,<br/>NetworkGraphView,<br/>RegionAndService<br/>AreaView, StationView"]:::readmodel
    R5["LaneView<br/>order-sourcing"]:::readmodel

    PL --> C6
    C6 --> G4
    G4 --> E8
    PL --> C7
    PL --> C8
    C7 --> N1
    C7 --> G2
    C8 --> G2
    G2 --> E6
    G2 --> E7
    PL --> C10
    C10 --> G2
    G2 --> E10
    PL --> C9
    C9 --> N1
    C9 --> G3
    G3 --> E9
    E6 --> R4
    E7 --> R4
    E8 --> R4
    E9 --> R4
    E10 --> R5
```

Source: `internal/domain/node/events.go`, `internal/domain/lane/events.go`, `internal/domain/servicearea/events.go`, `internal/domain/region/events.go`, `internal/application/usecases/*.go`.

Omitted: the refusals (`ErrLaneLoop`, `ErrLaneTypePairNotAllowed`,
`ErrLaneEndpointNotActive`, `ErrDuplicateLane`, `ErrNotADeliveryStation`,
`ErrStaleAssignmentVersion`, `region.ErrUnknownRegion`, `ErrRegionCodeTaken`). The `Node` sticky here
is read, not changed: the lane and service-area use cases load it to check
their invariants.

## Hotspots

None. The hotspots of the earlier design are answered in the code and
stated in the pages of this pack:

- lanes and service areas of a node that leaves service: policy
  `SuspendedEndpointsUnusable`;
- who declares a network node's profile: the network-ops planner, with
  `declaredBy` `NETWORK_OPS`;
- cutoff days: `CutoffSlot.Days`, default every operating day;
- a later revision of an `ACTIVE` fulfillment centre: it keeps at least one
  cutoff and a `HandlingTimeMinutes` above zero (`ErrIncompleteOperationalProfile`);
- region membership: `region.ErrUnknownRegion`;
- illegal transitions and unknown ids: the shared `ErrIllegalTransition` and
  `ports.ErrNotFound`.
