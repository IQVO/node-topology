# ADR 0018: lane emission declaration (gCo2e, planner-declared)

## Status

Accepted (2026-11-14).

## Context

The retail network records money on the model's seams since ADR 0014: a
planner declares the marginal cost of moving one package-unit across a lane,
and `order-sourcing` folds it into its money-denominated estimate. The
network roadmap (feature C9, carbon per shipment) calls for the same seam to
carry carbon so a shipment's footprint can be estimated from the declared
network instead of being invented downstream. The lane is the first carbon
seam: a planner already knows what moving one package across a lane on one
departure emits, and `model.yaml` is silent about that number (the model gap
recorded as A43 in ADR 0002, money; carbon has no row at all).

Two alternatives were weighed. Deriving the emission from measured fuel or
energy telemetry here: refused, this context owns declarations, not
measurements, and the declared-then-measured philosophy (ADR 0001 D3) keeps a
declaration a fact that is never re-derived. Waiting for
`linehaul-transportation` to publish measured emissions per departure:
refused for the same reason — the consumer needs a number now, and a later
measured event can supersede or calibrate it without this contract changing
shape.

## Decision

1. **A declared field on the Lane aggregate.** `emissionPerUnitG`, an `int64`
   of grams of CO2-equivalent: the planner's declared carbon emission of
   moving ONE package-unit across this lane on one departure. Default 0.
   Range 0 to 10000000 inclusive (`shared.Invalid` / 422
   `validation-failed` outside it). No unit field: the study network is
   single-unit (gCo2e), so the unit is an ecosystem constant, not a field.
2. **The command.** `DeclareLaneEmission`
   (`POST /lanes/{laneId}/emission-declarations`, `Idempotency-Key`
   mandatory) is valid on an OPEN lane only; a CLOSED lane is refused with
   the existing illegal-transition sentinel. Every declaration bumps the
   lane version and publishes `LaneEmissionDeclared`
   `{laneId, gCo2ePerUnit, declaredAt}` (CloudEvents catalogue ADR 0004),
   subject and Kafka key the `laneId`. A newer declaration supersedes the
   previous value; each declaration is a fact.
3. **Zero versus undeclared.** `0` is a legal declaration and means zero
   declared emission. A lane that was never declared on also reads `0`, but
   publishes nothing. A consumer therefore cannot tell the two apart from
   the number alone: an undeclared lane contributes `0` to a carbon sum AND
   leaves the consumer's estimate incomplete. What incomplete means is the
   consumer's decision (fall back to the ordinal penalty, flag the estimate);
   this context only promises the distinction is knowable — no event was
   published for that lane.
4. **Consumed by order-sourcing.** `order-sourcing` builds its local lane
   view from this event and records the declared grams beside the declared
   cents in its estimate (C9), leaving its ranking UNCHANGED: carbon is
   recorded, not optimised, in this slice. Consumers keep the latest
   declaration per `laneId`; CloudEvents `time` orders retries, the lane
   version orders intent.
5. **Excluded.** Per-mode nuance: a lane has exactly one mode (`GROUND` or
   `AIR`), so one number per lane is one number per mode; a carbon-budget
   policy lever that changes which path is chosen is the named follow-up,
   not this declaration.

## Consequences

- The REST lane reads (`GET /lanes`, `POST /lanes`) and the MCP `list_lanes`
  tool carry `emissionPerUnitG`; migration 0008 adds the column with the
  same range check, and legacy rows read `0` (undeclared).
- `order-sourcing` can estimate a path's carbon the moment a planner
  declares, with the incompleteness rule above as its only caveat.
- A declared emission is not a measurement and not an audit: it is stable
  until the next declaration, and nobody may re-derive it from anything.
- If the network ever needs measured emissions per departure, that is a new
  event from the measuring context, superseding nothing here.
