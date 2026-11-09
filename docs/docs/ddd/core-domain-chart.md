---
id: core-domain-chart
title: Core Domain Chart
sidebar_label: Core Domain Chart
description: Where Node Topology sits on the Core Domain Chart, and why it is a Supporting, custom-built enabler rather than a Core subdomain.
---

# Core Domain Chart

Following the ddd-crew [Core Domain Charts](https://github.com/ddd-crew/core-domain-charts):
business differentiation (y) against model complexity (x).

```mermaid
quadrantChart
    title node-topology on the Core Domain Chart
    x-axis Low model complexity --> High model complexity
    y-axis Low business differentiation --> High business differentiation
    quadrant-1 Core
    quadrant-2 Decisive - Short-term Core
    quadrant-3 Supporting
    quadrant-4 Generic
    node-topology context: [0.44, 0.42]
    Operational profile declaration: [0.47, 0.46]
    Lane and service-area graph: [0.38, 0.36]
    Node lifecycle and operator rule: [0.30, 0.30]
```

Source: `internal/domain/node`, `lane`, `servicearea`, `region` (aggregate sizes), and warehouse-docs `network/docs/strategic-design/core-domain-chart.md` (positions).

Omitted: the other nine contexts (each has its own chart; all ten are on the
domain-wide [Core Domain Chart](https://iqvo.github.io/warehouse-docs/network/strategic-design/core-domain-chart)).

## Classification: Supporting

All four points sit in **quadrant 3 (Supporting)**, bottom-left, just below
both midlines. The network model classifies `node-topology` as **Supporting** with
the business model "Enabler (every promise, assignment and route rests on
it)".

**Differentiation is moderate (y ≈ 0.42).** A shopper never sees a node
registry. What a shopper sees is the promise, and the promise is owned by
`delivery-promise` (Core) and kept by `order-sourcing` (Core). This context
differentiates only indirectly: a wrong cutoff, a missing lane or a
mis-assigned postal prefix produces promises the network cannot keep. That is
why it sits close to the midline rather than near the Generic floor. The
highest slice is the **operational profile declaration**, because a
versioned, declared handling time, cutoff set and throughput cap is what
lets the network promise from a vendor's building it does not run
([ADR 0003](https://iqvo.github.io/warehouse-docs/network/adr/0003-declared-and-measured-capability)).

**Model complexity is moderate (x ≈ 0.44).** There are four aggregates
(`Node`, `Lane`, `ServiceArea`, `Region`), one lifecycle with four states,
one with two, and two with a single state. There are seventeen domain errors
(`Err...` identifiers: nine on `Node`, four on `Lane`, two each on
`ServiceArea` and `Region`). The rules are crisp (allowed operator
per node type, allowed lane type pairs, strictly increasing revisions,
cutoffs inside operating hours). What lifts complexity is the cross-aggregate
checks: opening a lane reads both end nodes, assigning a service area reads
the station, and three uniqueness rules span many aggregates
(`ErrVendorNodeCodeTaken`, `ErrDuplicateLane`, `ErrRegionCodeTaken`). The
slice **node lifecycle and operator rule** is the simplest part.

## Business model

Enabler. It earns nothing on its own. It reduces the cost of every other
decision: `delivery-promise`, `order-sourcing`, `linehaul-transportation` and
`last-mile-delivery` each read the same node, lane and service-area facts
from one source, through events, instead of keeping their own registries.

## Evolution

| Part | Evolution stage | Why |
| --- | --- | --- |
| Node, lane, region and service-area registry | **Custom-built** | The node types, the operator rule and the lane type pairs are this network's own design ([ADR 0004](https://iqvo.github.io/warehouse-docs/network/adr/0004-parcel-nodes-are-network-operated)). |
| Operational profile | **Custom-built**, shaped after a **Product** | Modelled after the supply-source (multi-location inventory) API's `operationalConfiguration` (see the vendor platform API surface map), extended with cutoffs (per day), sort dwell (sort centres and air hubs only) and inbound limits ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)). |
| Master-data plumbing (outbox, CloudEvents, REST) | **Commodity** | The same building blocks every context uses ([event standard](https://iqvo.github.io/warehouse-docs/network/strategic-design/event-standard-cloudevents), [ADR 0005](/docs/adr/0005-outbox-relay-and-attempt-cap)). |

**Movement to watch.** If the network ever optimises its own topology
(choosing where to open a station, which lanes to run), that capability would
be differentiating and complex, and would land in quadrant 1. The network
model keeps it out: such a capability is not in any context, and the closest
named future context, `inventory-placement`, is about stock, not buildings.
Nothing in the code moves this context out of quadrant 3 today.
