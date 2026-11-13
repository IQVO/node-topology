---
id: 0016
title: Service-area delivery mode and declared carrier options
---

# Service-area delivery mode and declared carrier options

## Status

Accepted (2026-11-13)

## Context

The network's own last mile is not the only way to serve a postal prefix: in
sparse or remote areas the planner may hand packages to an external carrier at
a network node instead of routing them onto a station's delivery waves. No
repo of the fleet can compute whether own delivery pays — there is no density
model and no geography beyond declared constants — so the decision is made out
of band, by the planner, and lands in node-topology as a DECLARATION on the
service area. The per-package "chosen at the sort node by lowest cost" rule of
the model frame cannot be a free operator choice either: a promise quoted
against one carrier's transit must not be handed to another. Consumers of
`ServiceAreaAssigned` (the promise, the last mile) decode the event tolerantly
today, so the change must be additive on the same type.

## Decision

1. `ServiceArea` gains `mode DeliveryMode` — `NETWORK_ROUTE` (the zero value,
   and the behaviour of every existing area) | `CARRIER_HANDOFF` — and
   `carriers []CarrierOption` — required non-empty (1..
   `MaxCarriersPerArea` = 4) when `CARRIER_HANDOFF`, empty when
   `NETWORK_ROUTE`. `CarrierOption` is `{CarrierID string,
   HandoffNodeID node.ID, CostPerPackageCents int64, TransitMinutes int}`
   with `CostPerPackageCents` 0..10,000,000 (the same integer-cent ceiling a
   lane's declared cost uses, ADR 0014) and `TransitMinutes`
   1..2147483647.
2. `assign` enforces, besides the standing invariants:
   - `ErrHandoffNodeNotUsable` (409): each option's `HandoffNodeID` loads and
     is ACTIVE with type DELIVERY_STATION or SORTATION_CENTER — the two
     carrier-collect points reachable from the sort network under
     `allowedPairs`; a node of any other type, or inactive, is refused.
   - `ErrCarrierOptionsInvalid` (422): a duplicate `carrierId` within the
     area, an empty option list on CARRIER_HANDOFF, or a non-empty list on
     NETWORK_ROUTE.
3. Omission semantics on the wire: `deliveryMode` and `carriers` are
   `omitempty`; their absence means NETWORK_ROUTE with no options, which is
   the current behaviour. The change is additive v1 on the SAME type
   `com.retailnetwork.logistics.node-topology.servicearea.ServiceAreaAssigned`
   (same dataschema `urn:retailnetwork:node-topology:events:ServiceAreaAssigned:v1`),
   because every consumer decodes tolerantly and defaults to the old meaning.
4. Storage (migration `0006_service_area_delivery_mode`):
   `service_areas` gains `delivery_mode TEXT NOT NULL DEFAULT 'NETWORK_ROUTE'
   CHECK (delivery_mode IN ('NETWORK_ROUTE','CARRIER_HANDOFF'))` and
   `carrier_options JSONB NOT NULL DEFAULT '[]'`; the down migration drops
   both. Rollback: additive columns stay; unassigning is a reassignment to
   NETWORK_ROUTE (version+1).
5. REST: the assign request/response gain `deliveryMode` and
   `carriers[{carrierId, handoffNodeId, costPerPackageCents,
   transitMinutes}]` (additive, optional); 409 gains
   `handoff-node-not-usable`; 422 gains `carrier-options-invalid`; the
   resolve read returns them. MCP and A2A read tools surface the two fields
   additively.
6. The per-package carrier CHOICE is computed downstream from the declared
   options — sort by costPerPackageCents ascending, tie: the option with the
   LATER transit window, tie: lexicographically smallest carrierId — by the
   promise at quote time (into the witness) and by tracking at handoff time,
   both from the same area assignment version. The choice rule is not
   implemented in this repo and never chooses at the handoff desk.
7. Lanes are untouched: the handoff lane's declared cost is the planner's
   input to the mode declaration, not a promise input.

## Consequences

- Every area without a mode is NETWORK_ROUTE; nothing changes until a
  CARRIER_HANDOFF assignment exists (the C4 activation gate).
- Downstream repos (delivery-promise, last-mile-delivery) consume the mode
  from the same event; shipping-and-tracking does not consume it today.
- The planner owns the density decision; this context records it, it does
  not compute it.
- A reassignment can flip the mode either way under a strictly higher
  assignment version; consumers keep the highest version they have seen.
