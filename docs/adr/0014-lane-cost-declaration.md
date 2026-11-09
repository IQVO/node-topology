# ADR 0014: lane cost declaration (cents, planner-declared)

## Status

Accepted (2026-11-05).

## Context

The retail network's cost model is ordinal, not monetary: `order-sourcing`
scores a candidate path with `AssignmentCost = legs + crossRegionPenalty +
scorecardPenalty + bookPressurePenalty`, a proxy that cannot be compared with a
financial figure. The network roadmap (feature F7, money-denominated
cost-to-serve) calls for money on the model's seams so the proxy can become a
sum of cents. The lane is the first seam: a planner already knows what moving
one package across a lane on one departure costs them, and `model.yaml` is
silent about that number (the model gap recorded as A43 in ADR 0002).

Two alternatives were weighed. Deriving the cost from measured linehaul
invoices here: refused, this context owns declarations, not measurements, and
the declared-then-measured philosophy (ADR 0001 D3) keeps a declaration a fact
that is never re-derived. Waiting for `linehaul-transportation` to publish
measured cost per departure: refused for the same reason — the consumer needs
a number now, and a later measured event can supersede or calibrate it without
this contract changing shape.

## Decision

1. **A declared field on the Lane aggregate.** `costPerUnitCents`, an `int64`
   of integer cents: the planner's declared marginal cost of moving ONE
   package-unit across this lane on one departure. Default 0. Range 0 to
   10000000 inclusive (`shared.Invalid` / 422 `validation-failed` outside it).
   No currency field: the study network is single-currency, so the currency is
   an ecosystem constant, not a field.
2. **The command.** `DeclareLaneCost` (`POST /lanes/{laneId}/cost-declarations`,
   `Idempotency-Key` mandatory) is valid on an OPEN lane only; a CLOSED lane is
   refused with the existing illegal-transition sentinel. Every declaration
   bumps the lane version and publishes `LaneCostDeclared`
   `{laneId, costPerUnitCents, declaredAt}` (CloudEvents catalogue ADR 0004),
   subject and Kafka key the `laneId`. A newer declaration supersedes the
   previous value; each declaration is a fact.
3. **Zero versus undeclared.** `0` is a legal declaration and means zero
   declared marginal cost. A lane that was never declared on also reads `0`,
   but publishes nothing. A consumer therefore cannot tell the two apart from
   the number alone: an undeclared lane contributes `0` to a money sum AND
   leaves the consumer's estimate incomplete. What incomplete means is the
   consumer's decision (fall back to the ordinal penalty, flag the estimate);
   this context only promises the distinction is knowable — no event was
   published for that lane.
4. **Consumed by order-sourcing.** `order-sourcing` builds its local lane view
   from this event and folds the declared cents into its money-denominated
   `AssignmentCost` (F7). Consumers keep the latest declaration per `laneId`;
   CloudEvents `time` orders retries, the lane version orders intent.
5. **Excluded.** Currency modelling (single-currency study; a second currency
   would be a new ADR and a `:v2` dataschema) and per-mode nuance: a lane has
   exactly one mode (`GROUND` or `AIR`), so one number per lane is one number
   per mode; an air-versus-ground crossover rule is a consumer policy, not a
   topology fact.

## Consequences

- The REST lane reads (`GET /lanes`, `POST /lanes`) and the MCP `list_lanes`
  tool carry `costPerUnitCents`; migration 0005 adds the column with the same
  range check, and legacy rows read `0` (undeclared).
- order-sourcing can price a path in cents the moment a planner declares, with
  the incompleteness rule above as its only caveat.
- A declared cost is not a quote and not an invoice: it is stable until the
  next declaration, and nobody may re-derive it from anything.
- If the network ever needs measured cost per departure, that is a new event
  from the measuring context, superseding nothing here.
