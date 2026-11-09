---
id: class-diagram
title: Class Diagram
sidebar_label: Class Diagram
description: "UML class diagrams of Node Topology's domain model as built, and the hexagonal ports and adapters view of the Go service."
---

# Class Diagram

UML class diagrams of the domain model as built, in the Go hexagonal layout the
warehouse-systems fleet uses. Type names are the Go identifiers in
`internal/domain` (`node.Node`, `node.OperationalProfile`, `node.CutoffSlot`,
`node.ThroughputCap`, `lane.Lane`, `region.Region`,
`servicearea.ServiceArea`). Fields are unexported (`-`), methods exported
(`+`). Only state-changing methods and key queries are shown. Every state
change returns the raised events (`[]shared.Event`) and an error.

## 1. Node aggregate: root and operational profile

```mermaid
classDiagram
    direction TB
    class Node {
        <<AggregateRoot>>
        -id ID
        -typ Type
        -subtype Subtype
        -operator Operator
        -vendor VendorRef
        -regionID region.ID
        -timezone string
        -location GeoLocation
        -status Status
        -profile OperationalProfile
        -activatedAt Time
        -suspendedAt Time
        -suspendReason string
        -retiredAt Time
        -version int64
        +Register(in, reg, now)$ Node
        +DeclareProfile(d, now) error
        +Activate(now) error
        +Suspend(reason, now) error
        +Retire(now) error
        +AcceptsDemand() bool
        +Capabilities() Capabilities
    }
    class OperationalProfile {
        <<ValueObject>>
        -ProfileDeclaration embedded
        -DeclaredAt Time
        +Complete() bool
    }
    class ProfileDeclaration {
        <<ValueObject>>
        -Revision int
        -Timezone string
        -OperatingHours OperatingHours
        -Holidays string[]
        -HandlingTimeMinutes int
        -Cutoffs CutoffSlot[]
        -ThroughputCap ThroughputCap
        -SortDwellMinutes int optional
        -Inbound InboundLimits optional
        -DeclaredBy DeclaredBy
    }
    class CutoffSlot {
        <<ValueObject>>
        -LocalTime TimeOfDay
        -Days Weekday[]
        -MaxOrders int
    }
    class DayHours {
        <<ValueObject>>
        -Start TimeOfDay
        -End TimeOfDay
    }
    class ThroughputCap {
        <<ValueObject>>
        -Value int
        -TimeUnit TimeUnit
        -Unit string
    }
    class InboundLimits {
        <<ValueObject>>
        -MaxCartonWeightG int optional
        -MaxCartonDimsMm CartonDims optional
        -AcceptsPallets bool optional
        -RequiresSscc bool optional
    }
    class TimeUnit {
        <<Enumeration>>
        HOUR
        DAY
    }
    class DeclaredBy {
        <<Enumeration>>
        VENDOR
        NETWORK_OPS
    }
    class Status {
        <<Enumeration>>
        PROPOSED
        ACTIVE
        SUSPENDED
        RETIRED
    }
    Node *-- OperationalProfile : current revision 0..1
    Node --> Status
    OperationalProfile *-- ProfileDeclaration
    ProfileDeclaration *-- CutoffSlot : 0..*
    ProfileDeclaration *-- DayHours : OperatingHours map, 0..7
    ProfileDeclaration *-- ThroughputCap
    ProfileDeclaration *-- InboundLimits : 0..1
    ThroughputCap --> TimeUnit
    ProfileDeclaration --> DeclaredBy
```

Source: `internal/domain/node/node.go`, `profile.go`, `inbound.go`, `types.go`.

Omitted: the read accessors and the `Err...` variables (on
[Aggregate Design Canvas](/docs/ddd/aggregate-design-canvas)); `Snapshot`,
`Restore` and `Registration`, the persistence and input structs. The aggregate
holds only the **current** profile; the repository stores every past revision as
its own immutable row, which the aggregate never edits. `OperatingHours` is the
map `Weekday` to `DayHours` (`HH:mm`); a missing day is closed. `CutoffSlot.Days`
empty means every operating day; `MaxOrders` 0 means unset. `SortDwellMinutes`
is set only on `SORTATION_CENTER` and `AIR_HUB` nodes. `version` is the
optimistic-concurrency counter, not a domain field.

## 2. Node aggregate: identity, capabilities and type rules

```mermaid
classDiagram
    direction TB
    class Node {
        <<AggregateRoot>>
        -id ID
        -typ Type
        -operator Operator
        -vendor VendorRef
        -location GeoLocation
    }
    class Capabilities {
        <<ValueObject>>
        +Outbound bool
        +Inbound bool
        +Returns bool
        +Sort bool
        +LastMile bool
        +Air bool
    }
    class GeoLocation {
        <<ValueObject>>
        +PostalCode string
        +CountryCode string
        +Lat float optional
        +Lon float optional
    }
    class VendorRef {
        <<ValueObject>>
        +VendorID string
        +VendorNodeCode string
    }
    class Type {
        <<Enumeration>>
        FULFILLMENT_CENTER
        INBOUND_CROSS_DOCK
        RETURNS_CENTER
        SORTATION_CENTER
        DELIVERY_STATION
        AIR_HUB
        +Capabilities() Capabilities
        +AllowsOperator(o) bool
        +AllowsSubtype(s) bool
    }
    class Operator {
        <<Enumeration>>
        NETWORK
        VENDOR
    }
    class TypeRule {
        <<Policy>>
        +Operators Operator[]
        +Capabilities Capabilities
        +Subtypes Subtype[]
    }
    Node *-- GeoLocation
    Node *-- VendorRef : 0..1
    Node --> Operator
    Node --> Type
    Type ..> TypeRule : typeRules table
    TypeRule *-- Capabilities : DefaultCapabilitiesByType
```

Source: `internal/domain/node/types.go`, `node.go`.

Omitted: the fields already shown in diagram 1; the enumeration `Subtype`
(seven fulfillment-centre subtypes), listed on
[Ubiquitous Language](/docs/ddd/ubiquitous-language). `VendorRef` is absent on
a `NETWORK` node. Capabilities are not stored in the aggregate: `Capabilities()`
derives them from `Type` through the `typeRules` table (policy
`DefaultCapabilitiesByType`).

## 3. Lane aggregate

```mermaid
classDiagram
    direction TB
    class Lane {
        <<AggregateRoot>>
        -id ID
        -origin node.ID
        -destination node.ID
        -mode Mode
        -transit int
        -status Status
        -openedAt Time
        -closedAt Time
        -version int64
        +Open(in, origin, destination, now)$ Lane
        +Close(now) error
        +NominalTransitMinutes() int
    }
    class Mode {
        <<Enumeration>>
        GROUND
        AIR
    }
    class Status {
        <<Enumeration>>
        OPEN
        CLOSED
    }
    class Pair {
        <<ValueObject>>
        +Origin node.Type
        +Destination node.Type
    }
    Lane --> Mode
    Lane --> Status
    Lane ..> Pair : PairAllowed(o, d)
```

Source: `internal/domain/lane/lane.go`.

Omitted: `origin` and `destination` are node ids, not associations to `Node`;
`Open` receives the two loaded `Node` aggregates read-only. The type-pair rule is
the package-level list `allowedPairs` (ten pairs) behind `PairAllowed`, not a
class of its own.

## 4. ServiceArea and Region aggregates

```mermaid
classDiagram
    direction TB
    class ServiceArea {
        <<AggregateRoot>>
        -prefix PostalPrefix
        -station node.ID
        -region region.ID
        -assignment int
        -assignedAt Time
        -version int64
        +Assign(prefix, assignmentVersion, station, reg, now)$ ServiceArea
        +Reassign(assignmentVersion, station, reg, now) error
    }
    class Region {
        <<AggregateRoot>>
        -id ID
        -code string
        -name string
        -definedAt Time
        -version int64
        +Define(in, now)$ Region
    }
```

Source: `internal/domain/servicearea/servicearea.go`, `internal/domain/region/region.go`.

Omitted: `station` and `region` are ids, not associations: each aggregate is
loaded and saved on its own, and `Assign` takes the loaded station and region
read-only. `ServiceArea` and `Region` have a single state each (`ASSIGNED`,
`DEFINED`), so no status enumeration exists in code.

## 5. Domain events

```mermaid
classDiagram
    direction LR
    class Event {
        <<interface>>
        +EventName() string
        +Entity() string
        +SubjectID() string
        +AggregateVersion() int64
        +OccurredAt() Time
    }
    class NodeRegistered {
        <<DomainEvent>>
        NodeID, NodeType, Subtype, Operator
        VendorID, VendorNodeCode, RegionID
        Timezone, PostalCode, CountryCode
    }
    class NodeOperationalProfileDeclared {
        <<DomainEvent>>
        NodeID, ProfileRevision, Timezone
        OperatingHoursByDay, Holidays
        HandlingTimeMinutes, Cutoffs
        ThroughputCap, SortDwellMinutes
        Inbound, DeclaredBy
    }
    class NodeActivated {
        <<DomainEvent>>
        NodeID, ActivatedAt
    }
    class NodeSuspended {
        <<DomainEvent>>
        NodeID, Reason, SuspendedAt
    }
    class NodeRetired {
        <<DomainEvent>>
        NodeID, RetiredAt
    }
    class LaneOpened {
        <<DomainEvent>>
        LaneID, OriginNodeID, DestinationNodeID
        Mode, NominalTransitMinutes
    }
    class LaneClosed {
        <<DomainEvent>>
        LaneID, ClosedAt
    }
    class ServiceAreaAssigned {
        <<DomainEvent>>
        PostalPrefix, DeliveryStationNodeID
        RegionID, AssignmentVersion
    }
    class RegionDefined {
        <<DomainEvent>>
        RegionID, Code, Name
    }
    Event <|.. NodeRegistered
    Event <|.. NodeOperationalProfileDeclared
    Event <|.. NodeActivated
    Event <|.. NodeSuspended
    Event <|.. NodeRetired
    Event <|.. LaneOpened
    Event <|.. LaneClosed
    Event <|.. ServiceAreaAssigned
    Event <|.. RegionDefined
```

Source: `internal/domain/shared/shared.go`, `internal/domain/node/events.go`,
`internal/domain/lane/events.go`, `internal/domain/servicearea/events.go`,
`internal/domain/region/events.go`.

Omitted: field types (the wire shapes are on [Domain Events](/docs/ddd/domain-events)).
`SubjectID()` is the CloudEvents `subject` and the Kafka key; each event embeds
`shared.Header` (`Version`, `At`).

## 6. Hexagonal ports and adapters

```mermaid
%%{init: {"flowchart": {"nodeSpacing": 20, "rankSpacing": 40}}}%%
flowchart TB
    subgraph IN["inbound adapters"]
        HTTP["inbound/http<br/>REST, cmd/api"]
        MCP["inbound/mcp<br/>read-only tools, cmd/mcp"]
        A2A["inbound/a2a<br/>read-only skills, cmd/a2a"]
    end
    subgraph APP["application/usecases"]
        UN["9 commands: RegisterNode, DeclareOperationalProfile,<br/>ActivateNode, SuspendNode, RetireNode,<br/>OpenLane, CloseLane,<br/>AssignServiceArea, DefineRegion<br/>6 queries: GetNode, ListNodes, ListLanes,<br/>GetRegion, ListRegions, ResolveServiceArea"]
    end
    subgraph DOM["domain"]
        D1["node, lane,<br/>servicearea, region"]
    end
    subgraph PORTS["application/ports"]
        P1["NodeRepository<br/>LaneRepository<br/>ServiceAreaRepository<br/>RegionRepository"]
        P2["EventOutbox"]
        P3["UnitOfWork, Clock<br/>IDGenerator, NodeMetrics"]
    end
    subgraph OUT["outbound adapters"]
        A1["outbound/postgres<br/>repositories, UoW,<br/>outbox, migrations"]
        A2["outbound/outbox + outbound/kafka<br/>relay and Kafka sink"]
        A3["outbound/memory<br/>tests only"]
    end
    HTTP --> UN
    MCP --> UN
    A2A --> UN
    APP --> DOM
    APP --> PORTS
    P1 --- A1
    P2 --- A1
    P3 --- A1
    A1 --> A2
    P1 -.- A3
```

Source: `internal/application/ports/ports.go`, `internal/application/usecases/*.go`,
`cmd/api/run.go`, `cmd/mcp/main.go`, `cmd/a2a/main.go`.

Packages, under `internal/`:

| Package | Contents |
| --- | --- |
| `internal/domain/node` | `Node`, `OperationalProfile`, `ProfileDeclaration`, `CutoffSlot`, `ThroughputCap`, `InboundLimits`, `Capabilities`, `GeoLocation`, `VendorRef`, `Type`, `Subtype`, `Operator`, `Status`, the `typeRules` table, the nine `Err...` of `Node` |
| `internal/domain/lane` | `Lane`, `Mode`, `Status`, `Pair`, `allowedPairs`, the four `Err...` of `Lane` |
| `internal/domain/servicearea` | `ServiceArea`, `PostalPrefix`, `ErrNotADeliveryStation`, `ErrStaleAssignmentVersion` |
| `internal/domain/region` | `Region`, `ErrRegionCodeTaken`, `ErrUnknownRegion` |
| `internal/domain/shared` | the `Event` interface, `Header`, `ErrValidation`, `ErrIllegalTransition`, id and text validators |
| `internal/application/usecases` | the nine command use cases and the six query use cases |
| `internal/application/ports` | the four repositories, `EventOutbox`, `UnitOfWork`, `Clock`, `IDGenerator`, `NodeMetrics`, `ErrNotFound`, `ErrConcurrentUpdate` |
| `internal/adapters/inbound/http` | the sixteen REST operations, `/healthz` and `/readyz`, problem+json mapping, `Idempotency-Key` handling |
| `internal/adapters/inbound/mcp`, `inbound/a2a` | the read-only MCP tools and A2A skills, over the six query use cases |
| `internal/adapters/outbound/postgres` | repositories, unit of work, outbox writer and drain, migrations |
| `internal/adapters/outbound/outbox`, `outbound/kafka` | the relay loop; the Kafka writer, encoder and relay sink to `retailnetwork.node-topology.events` |
| `internal/adapters/kafka/cloudevents` | the CloudEvents envelope |
| `internal/adapters/outbound/memory` | in-memory repositories for unit tests; `cmd/**` never imports them (a fitness test pins it) |

Omitted from the diagram: the composition roots (`cmd/`), `outbound/telemetry`,
`outbound/clock`, `outbound/ids`, and the read of a second aggregate (`OpenLane`
and `AssignServiceArea` read `Node` through `NodeRepository`).
