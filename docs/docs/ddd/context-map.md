---
id: context-map
title: Context Map
sidebar_label: Context Map
description: Node Topology's neighbourhood on the retail-network context map, with the upstream and downstream side, pattern and channel of every relationship.
---

# Context Map

Following ddd-crew [Context Mapping](https://github.com/ddd-crew/context-mapping).
This is **node-topology's slice** of the
[domain-wide context map](https://iqvo.github.io/warehouse-docs/network/strategic-design/context-map). Node Topology is
upstream of every relationship it has: it consumes nothing and calls nobody.

## Event relationships

```mermaid
flowchart LR
    NT["node-topology<br/>Supporting"]
    OA["offer-availability<br/>Supporting"]
    DP["delivery-promise<br/>Core"]
    OS["order-sourcing<br/>Core"]
    LH["linehaul-transportation<br/>Supporting"]
    LM["last-mile-delivery<br/>Supporting"]
    NP["node-performance<br/>Supporting"]
    VC["vendor-connectivity<br/>Supporting"]
    SI["seller-inbound"]

    NT -->|"U to D: OHS+PL / CF<br/>Kafka, 4 node events"| OA
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, all 9 events"| DP
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, all 9 events"| OS
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, 5 node + 2 lane events"| LH
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, NodeRegistered,<br/>ServiceAreaAssigned"| LM
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, NodeRegistered,<br/>NodeOperationalProfileDeclared"| NP
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, 5 node events"| VC
    NT -->|"U to D: OHS+PL / CF<br/>Kafka, 5 node events"| SI
```

Source: `apis/asyncapi.yaml`, `cmd/api/run.go`, `cmd/api/relay.go` and warehouse-docs `network/docs/strategic-design/context-map.md` (neighbours).

## Synchronous relationships and Separate Ways

```mermaid
flowchart LR
    VC["vendor-connectivity<br/>Supporting"]
    NT["node-topology<br/>Supporting"]
    PL(["network-ops planner<br/>person"])
    ANY(["any context<br/>or client"])
    NF[["network-fulfillment<br/>fleet, external<br/>Supporting in the fleet"]]
    CO["customer-ordering<br/>Generic"]
    ST["shipping-and-tracking<br/>Generic"]

    NT -->|"U to D: C/S<br/>VC is Customer<br/>REST POST /nodes,<br/>PUT .../operational-profile"| VC
    NT -->|"U to D: UI<br/>REST nodes, lanes,<br/>service-areas, regions"| PL
    NT -.->|"U to D: OHS<br/>REST, MCP, A2A reads:<br/>nodes, lanes, service-areas, regions"| ANY
    NF -. "Separate Ways<br/>Vendor API only" .- NT
    CO -. "Separate Ways" .- NT
    ST -. "Separate Ways" .- NT
```

Source: `apis/openapi.yaml`, `internal/adapters/inbound/http/server.go`, `cmd/api/run.go` and warehouse-docs `network/docs/strategic-design/context-map.md` (neighbours).

Legend: arrows point from upstream (U) to downstream (D). Solid = command or
event, dotted with an arrow head = query, dotted without an arrow head = no
relationship (Separate Ways). Pattern abbreviations: OHS = Open Host Service,
PL = Published Language, CF = Conformist, C/S = Customer/Supplier. Path
parameters are written without braces inside the diagrams.

Omitted: Kafka and Postgres as infrastructure; the analytics topic (nothing is
published on it); the future contexts (`customer-returns`,
`customer-notification`, `payments-and-invoicing`), which have no edge here.

## Relationships

| Neighbour | Direction | Pattern | Channel | Messages | Why this pattern |
| --- | --- | --- | --- | --- | --- |
| `offer-availability` (Supporting) | NT upstream, OA downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `NodeActivated`, `NodeSuspended`, `NodeRetired` | OA only needs to know whether a listing's node is sellable (`NodeStatusView`, `SuppressOnNodeUnavailable`). It takes the node vocabulary as published. |
| `delivery-promise` (Core) | NT upstream, DP downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | all nine events | DP builds `NodeProfileView`, `RegionAndServiceAreaView` and `LaneView` locally because its hot path makes no synchronous call. The topology is a given for the promise, not a negotiation. |
| `order-sourcing` (Core) | NT upstream, OS downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | all nine events | OS builds `NodeProfileView` and `NetworkGraphView` to re-optimise assignments. Same reasoning as DP. |
| `linehaul-transportation` (Supporting) | NT upstream, LH downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`, `NodeSuspended`, `NodeRetired`, `LaneOpened`, `LaneClosed` | LH turns lanes into departures and cutoffs into pickup departures (`PickupFromCutoffs`). It conforms to the lane and cutoff definitions. |
| `last-mile-delivery` (Supporting) | NT upstream, LM downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `ServiceAreaAssigned` | LM's `StationView` needs the stations and the prefixes each serves. |
| `node-performance` (Supporting) | NT upstream, NP downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `NodeOperationalProfileDeclared` | NP measures a node against what it declared; it needs the declaration as published. |
| `vendor-connectivity` (Supporting) | NT upstream, VC downstream | OHS + PL / Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`, `NodeSuspended`, `NodeRetired` | VC's `VendorNodeView` lets it answer vendors about their nodes without calling this context. |
| `vendor-connectivity` (Supporting) | NT upstream (Supplier), VC downstream (Customer) | Customer/Supplier | REST in-org `POST /nodes`, `PUT /nodes/{nodeId}/operational-profile` | `RegisterNode`, `DeclareOperationalProfile` | VC needs an answer inside the processing of a vendor submission. The routes exist for this customer; the Supplier keeps the invariants (`ErrOperatorNotAllowedForNodeType`, `ErrStaleProfileRevision`) and VC turns a refusal into a failed submission. |
| network-ops planner (person) | NT upstream | UI | REST `/nodes`, `/lanes`, `/service-areas`, `/regions` | `RegisterNode` (network node), `DeclareOperationalProfile` (`declaredBy` `NETWORK_OPS`), `ActivateNode`, `SuspendNode`, `RetireNode`, `OpenLane`, `CloseLane`, `AssignServiceArea`, `DefineRegion` | A person drives the network's shape; there is no upstream system for it. |
| any context or client | NT upstream | Open Host Service | REST `GET /nodes/{nodeId}`, `GET /nodes?type=&region=&status=`, `GET /lanes?origin=&destination=`, `GET /service-areas/{postalPrefix}`, `GET /regions`, `GET /regions/{regionId}`; the six MCP tools; the two A2A skills | `GetNode`, `ListNodes`, `ListLanes`, `ResolveServiceArea`, `ListRegions`, `GetRegion` | One public read protocol for everyone. No neighbour is known to call these synchronously; they serve tools, planners and diagnosis. |
| `network-fulfillment` (warehouse-systems fleet, Supporting in the fleet, external) | none | Separate Ways | none | none | The hard boundary: the fleet reaches the network only through the Vendor API of `vendor-connectivity` ([ADR 0007](https://iqvo.github.io/warehouse-docs/network/adr/0007-cloudevents-namespace), [WMS/WES integration](https://iqvo.github.io/warehouse-docs/network/integration/wms-wes-integration)). Nothing in the service code or contracts refers to the fleet. |
| `customer-ordering` (Generic) | none | Separate Ways | none | none | It works with promises and postal prefixes, never with nodes. |
| `shipping-and-tracking` (Generic) | none | Separate Ways | none | none | Scans name a `nodeId` as a value; it keeps no node read model. |

## Notes on the patterns

- **Open Host Service + Published Language, every consumer Conformist.** One
  topic, nine CloudEvents types, one schema per type
  (`urn:retailnetwork:node-topology:events:<EventName>:v1`). With eight
  consumers, negotiating a model per consumer would cost more than it gives.
  The vocabulary is small and stable, so conforming is cheap.
- **Customer/Supplier with `vendor-connectivity`, not Conformist.** The two
  write routes exist because `vendor-connectivity` needs them, and their
  error contract (a 409 or 422 that becomes a failed vendor submission) is
  agreed between the two.
- **No Anti-Corruption Layer here.** The fleet's vocabulary (`siteCode`,
  `siteRole`, CPT) never arrives in this context. Translation happens twice,
  outside it: in the fleet's `network-fulfillment` (Conformist + ACL to the
  Vendor API) and in `vendor-connectivity`'s Published Language.
- **No Shared Kernel.** Consumers re-declare the few node fields they need
  in their own read models.

## Seller inbound

The `seller-inbound` edge is the eighth subscriber. Inbound limits are built as
[ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile) decided;
the `INBOUND` capability of `FULFILLMENT_CENTER` and the gate
`ErrInboundLimitsNotAllowed` are not built (the network model's gap G64), and
the service value-checks the limits on any node type.

| Neighbour | Direction | Pattern | Channel | Messages | Why |
| --- | --- | --- | --- | --- | --- |
| `seller-inbound` | U → D | Conformist | Kafka `retailnetwork.node-topology.events` | `NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`, `NodeSuspended`, `NodeRetired` | A node's status, type and carton limits are this context's facts. `seller-inbound` keeps a local `NodeInboundView`. |
