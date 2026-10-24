# ADR 0007: inbound limits on the operational profile

## Status

Accepted (2026-10-24).

## Context

Sellers will ship cartons to the network's nodes (the `seller-inbound` context).
Before a carton is labelled and shipped, someone must check it against what the
destination node can physically receive: its weight, its size, whether pallets
are taken and whether every carton must carry an SSCC. Node Topology already
owns what a node declares about itself in the versioned `OperationalProfile`
(ADR 0002, ADR 0003 decision 5), so the limits belong there. The consumer must
not call this service (ADR 0003: no sync calls between network contexts), so
the limits have to travel on the existing profile event.

## Decision

1. **A value object on the profile.** `OperationalProfile` gains an optional
   `inbound` of type `InboundLimits`: `maxCartonWeightG` (grams),
   `maxCartonDimsMm{l, w, h}` (millimetres), `acceptsPallets` and `requiresSscc`.
   Quantities are integers, never decimals. It is part of the immutable profile
   revision: a new revision restates the limits or declares none.
2. **Invariants** (all in the domain, `ErrValidation` / 422 `validation-failed`).
   Every number that is set is a positive integer, at most 2147483647 (zero and
   negatives refused). The three edges of `maxCartonDimsMm` are all-or-none: a
   missing edge is refused, never defaulted. An `inbound` that declares nothing
   (all four fields absent) is refused: omit it instead.
3. **Absent means unchecked.** Every field is optional on its own. A field that
   is absent was not declared, and so is not a limit: `acceptsPallets` absent is
   not "no pallets", it is "unchecked". A profile without `inbound` declares no
   limits at all. The limits are declared here and only here.
4. **Declared here, consumed as a local view.** `seller-inbound` keeps its own
   copy of the limits, fed by `NodeOperationalProfileDeclared`, and validates
   cartons against that copy. It keeps the highest `profileRevision`
   (the rule every consumer of this event already follows). A node whose latest
   revision has no `inbound` therefore has no limits.
5. **REST.** `PUT /nodes/{nodeId}/operational-profile` accepts `inbound`, and
   every read that returns a profile (`GET /nodes`, `GET /nodes/{nodeId}`, the
   `PUT` response) echoes it. Additive: a client that never sends it keeps the
   same contract. Optional means omitted, never `null` on the wire we write; an
   explicit `null` in a request is read as absent. Unknown keys inside `inbound`
   stay a 400 like everywhere else (the strict decoder).
6. **Event.** `NodeOperationalProfileDeclared` carries `inbound` (camelCase, same
   shape, only the declared fields) when the revision set it, and omits the key
   otherwise. The event stays `v1`: the change is additive and old consumers
   ignore the key. `apis/asyncapi.yaml` documents it with an example.
7. **Storage.** Migration `0003_profile_inbound_limits` adds a nullable `inbound`
   JSONB column to `node_profile_revisions` (an object holding only the declared
   limits; SQL NULL = none). A JSONB document, like the cutoffs and operating
   hours beside it, because the limits are always read and written with their
   revision and never queried by field. Existing rows keep NULL; the
   append-only trigger is untouched (it guards rows, not the table shape). The
   adapter owns its own document struct, as for the other JSONB columns.

## Consequences

- No new endpoint, topic, event, consumer or environment variable; the chart is
  unchanged.
- A node that never declared limits keeps accepting everything, so rolling this
  out changes no behaviour until a profile sets `inbound`.
- Limits are per profile revision, so history is kept: earlier revisions still
  show what the node declared then.
- Open for `seller-inbound`: what it does with cartons of a node whose limits it
  has not yet received (unchecked, per decision 3).
