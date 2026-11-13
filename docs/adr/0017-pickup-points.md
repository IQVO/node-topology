---
id: 0017
title: Pickup points and their serving delivery station
---

# Pickup points and their serving delivery station

## Status

Accepted (2026-11-13). Extends the node model under the amended RN-0004
(RN-0011: parcel nodes stay network-operated; a pickup point is a new node
type operated NETWORK or PARTNER).

## Context

Out-of-home delivery cuts failed attended-delivery attempts: many packages for
one point take one stop of last-mile capacity, and a shopper who is not home
still gets the package. Partner-operated drop-off networks extend coverage
without the network building buildings. The checkout destination today is
always a ship-to address, which carries the PII the network confines.

## Decision

- A new node type `PICKUP_POINT` with the `COLLECTION` capability, derived
  from the type like every capability, never set by a caller.
- A third operator `PARTNER`, allowed ONLY on `PICKUP_POINT`. A PARTNER point
  carries a `vendorRef` (the operating partner's id and the point code) on the
  existing nullable wire fields — the same shape a VENDOR node carries. A
  NETWORK point carries none.
- A `PICKUP_POINT` registration names exactly one `servingStationNodeId`: an
  ACTIVE `DELIVERY_STATION`, the same invariant a service area enforces. Every
  other node type carries none (`ErrServingStationNotAllowed`).
- The point's own `postalCode` must be uppercase letters and digits only —
  the label charset — because the point's code rides its labels
  (`ErrPointPrefixNotLabellable`). Prefixes with hyphens are fine elsewhere in
  the model; a point cannot have one.
- `lane_rules` and `allowedPairs` are untouched: a pickup point appears in no
  pair, so it can never be a lane endpoint. It is a route stop of its serving
  station, never a lane end.
- `NodeRegistered` gains a nullable `servingStationNodeId` (always present,
  null when not applicable — the vendorId pattern).

## Consequences

- Lockers are modelled as pickup points without compartment inventory in this
  decision; per-compartment holds and oversize rules are a named follow-up.
- Consumers derive `collection` from `nodeType` like every capability; nothing
  changes for the six existing types.
- The activation is data: nothing changes anywhere until the first
  `PICKUP_POINT` is registered.
