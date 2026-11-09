---
id: ubiquitous-language
title: Ubiquitous Language
sidebar_label: Ubiquitous Language
description: "The terms Node Topology speaks, the Go identifiers that implement them, the neighbour terms it uses, and how each maps to the warehouse-systems fleet's vocabulary."
---

# Ubiquitous Language

The terms this context speaks, each with the Go identifier that implements it
in `internal/domain`. The domain-wide glossary is on
[Ubiquitous Language](https://iqvo.github.io/warehouse-docs/network/strategic-design/ubiquitous-language).

Source: `internal/domain/node/{node,types,profile,inbound}.go`,
`internal/domain/lane/lane.go`, `internal/domain/servicearea/servicearea.go`,
`internal/domain/region/region.go`.

## Context terms

| Term | Definition | Code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Node** | One physical building of the network, of exactly one node type. | `node.Node` | A fleet **site** (`siteCode`): the fleet's own record of a building it runs. One vendor node corresponds to one fleet site only through the fleet's persisted mapping. |
| **Node Type** | `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`, `RETURNS_CENTER`, `SORTATION_CENTER`, `DELIVERY_STATION` or `AIR_HUB`. | `node.Type` | The fleet's `siteRole` (finer grained, for example `ROBOTICS_SORTABLE_FC`); it maps to a node type plus a subtype. |
| **Node Operator** | Who runs the building: `NETWORK` or a `VENDOR`. | `node.Operator` | **Declared by** (`VENDOR` or `NETWORK_OPS`, `node.DeclaredBy`): who declared one profile revision. |
| **Operational Profile** | The node's declared operating hours, cutoffs, handling time, throughput cap and time zone, versioned by its revision. | `node.OperationalProfile` | The **Scorecard** (`node-performance`): measured, not declared. |
| **Cutoff** | A local time by which a node declares packages are ready to be handed over; the network schedules one pickup departure per cutoff. | `node.CutoffSlot` | **Required Ship-By** (`vendor-connectivity`): the one cutoff instant a purchase order must meet. The fleet's **CPT**: see the fleet translation below. |
| **Handling Time** | Declared time from order release to a package ready for handover. | `node.ProfileDeclaration.HandlingTimeMinutes` | Nominal transit (a lane's travel time) and the fleet's `cycleTimeP95` (a measured per-path figure). |
| **Sort Dwell** | Declared minutes a package normally spends inside a sortation centre or air hub between arrival and departure; declared only by those node types (`ErrSortDwellNotAllowed`). | `node.ProfileDeclaration.SortDwellMinutes` | Handling Time. |
| **Throughput Cap** | Declared maximum orders per time unit (`HOUR` or `DAY`) a node accepts. | `node.ThroughputCap` | A departure's package capacity (`linehaul-transportation`) and a promise book's share (`delivery-promise`). |
| **Lane** | A directed, mode-specific connection between two nodes. | `lane.Lane` | A **Departure** (one scheduled trip on a lane) and a **Lane Schedule** (the weekly template), both owned by `linehaul-transportation`. |
| **Region** | A largely self-sufficient partition of the network. | `region.Region` | A `region` line of a ship-to address in `customer-ordering` (an address field, not a network partition). |
| **Service Area** | The postal prefix one delivery station serves, versioned by an assignment version. | `servicearea.ServiceArea` | A delivery wave's window (`last-mile-delivery`); a service area says *who* serves a prefix, not *when*. |

## Further terms

Terms that appear in this context's value objects, invariants and events but
are not aggregates of their own.

| Term | Definition | Code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Node Subtype** | A refinement of `FULFILLMENT_CENTER`: `SORTABLE`, `NON_SORTABLE`, `XL`, `SOFT_LINES`, `SUB_SAME_DAY`, `FRESH`, `MICRO` (`ErrSubtypeNotAllowed` for any other type). | `node.Subtype` | Node Type. |
| **Node Capabilities** | `Outbound`, `Inbound`, `Returns`, `Sort`, `LastMile`, `Air`, derived from the node type (policy `DefaultCapabilitiesByType`) and never set by a caller. | `node.Capabilities`, `node.Type.Capabilities()` | A vendor's claimed availability (`offer-availability`). |
| **Node Status** | `PROPOSED`, `ACTIVE`, `SUSPENDED`, `RETIRED`. | `node.Status` | The lane status `OPEN` / `CLOSED` (`lane.Status`). |
| **Parcel node** | A `SORTATION_CENTER`, `DELIVERY_STATION` or `AIR_HUB`; always operator `NETWORK`. | `node.Type` values | A vendor node. |
| **Profile Revision** | The strictly increasing version of a node's operational profile (`ErrStaleProfileRevision`). | `node.ProfileDeclaration.Revision`, table `node_profile_revisions` | The aggregate's optimistic-concurrency version (`Node.Version()`). |
| **Operating Hours by Day** | The hours a node works on each day of the week, in its time zone: weekday to `{start, end}` with `HH:mm` times; a missing day is closed. | `node.OperatingHours`, `node.DayHours` | Holidays (`ProfileDeclaration.Holidays`): ISO dates on which the node is closed all day. |
| **Vendor Node Code** | The vendor's own code for one of its nodes, unique per vendor (`ErrVendorNodeCodeTaken`). | `node.VendorRef.VendorNodeCode` | `nodeId` (`node.ID`), which the network mints. |
| **Lane Mode** | `GROUND` or `AIR`. | `lane.Mode` | Departure kind (`PICKUP`, `LINEHAUL`, `AIR`) in `linehaul-transportation`. |
| **Nominal Transit** | A lane's nominal travel time in minutes. Only `STANDARD` promises may fall back on it. | `lane.Lane.NominalTransitMinutes()` | A scheduled departure's real `departsAt` to `arrivesAt`. |
| **Assignment Version** | The strictly increasing version of a postal prefix's service-area assignment (`ErrStaleAssignmentVersion`). | `servicearea.ServiceArea.AssignmentVersion()` | `assignmentVersion` of a `FulfillmentAssignment` in `order-sourcing`, which shares the name and the error `ErrStaleAssignmentVersion` but nothing else. |
| **Postal Prefix** | The key of a service area: 1 to 16 characters from `A-Z`, `0-9` and `-`, matched exactly and never normalised. | `servicearea.PostalPrefix` | A full postal code (`node.GeoLocation.PostalCode`). |
| **Inbound Limits** | A receiving node's declared carton weight and size limits, whether it accepts pallets and whether it requires an SSCC on every carton ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)). | `node.InboundLimits`, `node.CartonDims` | A purchase order's `requiredShipBy`, which is about outbound handover. |

## Neighbour terms used here

Owned by other contexts; the identifiers are theirs and are not checked
against this repository.

| Term | Definition | Owner and code identifier | Not to be confused with |
| --- | --- | --- | --- |
| **Vendor** | An organisation operating fulfillment nodes for the network (the warehouse-systems fleet is vendor `V-WS`). | `vendor-connectivity`, `vendor.VendorAccount` | Node Operator `VENDOR`, the value on a node. |
| **Vendor API** | The Open Host Service of `vendor-connectivity`; its JSON schemas are the Published Language. | `vendor-connectivity`, `apis/vendor-connectivity/openapi.yaml` | This context's own REST API, which only `vendor-connectivity` and network roles call. |
| **Submission / Transaction** | An asynchronous vendor write and its outcome, reconciled by `transactionId`. | `vendor-connectivity`, `submission.Submission` | A synchronous `POST /nodes` here, which a submission triggers. |
| **Required Ship-By** | The pickup cutoff a purchase order must be ready for; the fleet calls it the CPT. | `vendor-connectivity`, `po.PurchaseOrder.RequiredShipBy` | Cutoff (the recurring local time declared here). |
| **Pickup Departure** | The departure that collects packages at a vendor node at one of its declared cutoffs. | `linehaul-transportation`, `departure.KindPickup` | Cutoff, which it is generated from. |
| **Departure** | One scheduled trip on a lane with a cutoff and a capacity. | `linehaul-transportation`, `departure.Departure` | Lane. |
| **Delivery Wave** | A station's dispatch slot: induct cutoff, delivery window and stop capacity. | `last-mile-delivery`, `wave.DeliveryWave` | Service Area. |
| **Scorecard** | A node's measured performance over a rolling window, with a grade. | `node-performance`, `scorecard.NodeScorecard` | Operational Profile. |
| **Declared vs Measured** | What a vendor says (profile) against what the network observes (scorecard). | `node-performance`, principle | |
| **Service Tier** | `SAME_DAY`, `NEXT_DAY`, `TWO_DAY` or `STANDARD`. | `delivery-promise`, `promise.Tier` | Node subtype `SUB_SAME_DAY`, which is one input to the `SAME_DAY` path. |
| **Listing** | A catalogue item offered from one node. | `offer-availability`, `listing.Listing` | Node. |

## Fleet translation

Network term against the warehouse-systems fleet term. The canonical table
for the whole seam is on [WMS/WES integration](https://iqvo.github.io/warehouse-docs/network/integration/wms-wes-integration);
these rows are the ones this context owns. The fleet never sees this
context: every row crosses through the Vendor API of `vendor-connectivity`,
and the fleet's `network-fulfillment` translates.

| Network term (this context) | warehouse-systems term | How they relate |
| --- | --- | --- |
| `nodeId` | `siteCode` (`facility-layout`) | The network mints `nodeId` when it registers the node. The fleet's `network-fulfillment` holds the `nodeId` to `siteCode` correspondence as a **persisted mapping**, never as a string convention. |
| `vendorNodeCode` | chosen by `network-fulfillment` for the site | The vendor's own code for the node, unique per vendor. The network never parses it and never derives a `siteCode` from it. |
| Node Type + Subtype | `siteRole` (`facility-layout`) | Mapped by the fleet-site-role table on the aggregator's [Business Context](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/business-context) page. |
| Parcel node (`SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB`) | `siteRole` `SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB_GATEWAY` | Network-operated. The fleet's rows are reference mirrors only and are never registered. |
| Cutoff | CPT (Critical Pull Time) | A network cutoff is the local time a vendor node declares packages ready for a pickup departure. The fleet plans its floor against its CPT schedule. A purchase order's `requiredShipBy` is one cutoff instant, which the fleet reads as a CPT. |
| Handling Time | `cycleTimeP95` (`process-path-management`) | Handling Time is one declared node-level figure. `cycleTimeP95` is a measured per-path percentile inside the fleet. The fleet may base its declaration on it; the network never sees `cycleTimeP95`. |
| Operational Profile | the fleet's declaration through `PUT /vendor/nodes/{nodeId}/profile` | The fleet composes the profile; this context stores each revision as declared (`declaredBy` `VENDOR`). |

## Inbound limits

The limits are built as [ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)
decided and gated as [ADR 0015](/docs/adr/0015-fulfillment-centres-receive-inbound) decided: only a node type with the
`INBOUND` capability (`FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`) declares them;
any other type is `ErrInboundLimitsNotAllowed` (422 `inbound-limits-not-allowed`),
and the values are checked (`ErrValidation`, 422 `validation-failed`) only after
the gate. `FULFILLMENT_CENTER` carries `INBOUND` beside `OUTBOUND`, which closes
the network model's gap G64 (see [Integration](/docs/ecosystem/integration#seller-inbound-and-open-gaps)).
