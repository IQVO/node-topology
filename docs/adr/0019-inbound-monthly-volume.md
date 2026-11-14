# ADR 0019: inbound monthly volume limit (dm3, node-declared)

## Status

Accepted (2026-11-14).

## Context

A node's declared inbound limits carry grams, millimetres and booleans only
(ADR 0007): the heaviest carton, the largest carton, pallets, SSCC. None of
them says how much inbound VOLUME the node accepts, yet the network roadmap
(feature C7, inbound capacity allocation) needs exactly that number before
seller-inbound can refuse an over-allocated plan. `model.yaml` is silent
about volumetric capacity. The gate's rule (wave 4 decision 3) is
record-and-allow: the limit is enforced only when the node declares it AND
every plan line carries a unit volume; a legacy plan is never blocked.

Two alternatives were weighed. Deriving the volume from the node's
throughput cap: refused, throughput is units per time, not volume per
month, and the declared-then-measured philosophy (ADR 0001 D3) keeps a
declaration a fact that is never re-derived. Waiting for
warehouse-planning to publish measured dock capacity: refused for the same
reason — seller-inbound needs a number now, and a later measured figure can
supersede or calibrate it without this contract changing shape.

## Decision

1. **A declared field on the inbound limits.** `monthlyVolumeDm3`, an
   optional `*int` of cubic decimetres: the inbound volume the node accepts
   per calendar month. Range 1 to 2147483647 (`shared.Invalid` / 422
   `validation-failed` outside it; the Postgres integer ceiling, the same
   `MaxInboundLimit` every inbound limit reuses). Optional exactly like its
   siblings: absent means not declared, and the all-nil rule of ADR 0007
   now counts it — an `inbound` object with `monthlyVolumeDm3` alone is a
   valid declaration.
2. **The wire.** `NodeOperationalProfileDeclared.inbound.monthlyVolumeDm3`
   is optional and omitted when undeclared (additive, decode-tolerant); the
   REST `declareNodeProfile` body and the node view carry the same optional
   key. Inbound limits remain declarable only by node types with the
   INBOUND capability (`ErrInboundLimitsNotAllowed` unchanged).
3. **Storage.** Migration 0009 adds the nullable column
   `node_profile_revisions.monthly_volume_dm3 INTEGER NULL CHECK
   (monthly_volume_dm3 BETWEEN 1 AND 2147483647)` beside the JSONB `inbound`
   document of ADR 0007 (the table's real name at branch time; the column
   is first-class because it carries its own range check, unlike the
   grab-bag JSONB). Legacy rows keep NULL and read as undeclared. Profile
   revisions stay append-only rows, never edited.
4. **Consumed by seller-inbound.** `seller-inbound` builds its node inbound
   view from the event and enforces the monthly gate against the seller's
   scorecard tier (C7): 100% GOOD / 80% WATCH / 50% AT_RISK of the declared
   dm3. Either side unknown (no declared limit, or a plan line without a
   unit volume) means the gate is recorded unenforced, never blocking — a
   legacy plan keeps working on day one.
5. **Excluded.** Daily or weekly granularity (a month is the allocation
   key the roadmap names; a finer key would be a new ADR), and any
   execution-side capacity use: this context declares the number, it does
   not schedule docks.

## Consequences

- A node declares one number and seller-inbound can refuse an
  over-allocated plan the same day, with the record-and-allow rule above as
  its only caveat.
- The REST declare-profile body, the node view and the event payload gain
  one optional key; consumers that predate it decode the event with their
  old meaning.
- A declared monthly volume is a promise about a calendar month, not a
  reservation: it says what the node accepts, never who got it. Allocation
  state belongs to seller-inbound.
- If the network ever needs measured volumetric throughput, that is a new
  event from the measuring context, superseding nothing here.
