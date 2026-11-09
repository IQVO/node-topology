# ADR 0015: Fulfillment centres receive inbound

## Status

Accepted (2026-11-08). Amends the capability table of
[ADR 0002](./0002-aggregates-and-invariants.md) and adds a capability gate to
[ADR 0007](./0007-inbound-limits-on-the-operational-profile.md).

## Context

The network model's `node_types` table (`model.yaml`) and the network ADR on
seller inbound give `FULFILLMENT_CENTER` the capabilities `[OUTBOUND, INBOUND]`:
a seller's cartons are received at the building that stores them.
`INBOUND_CROSS_DOCK` already had `INBOUND`. `seller-inbound` already treats
both types as receiving nodes. This service's `typeRules`
(`internal/domain/node/types.go`) still gave `FULFILLMENT_CENTER` only
`OUTBOUND`, so `GET /nodes/{nodeId}` reported `inbound: false` for a node that
receives seller cartons. The capability gate the design calls for was missing
too: ADR 0007 value-checks inbound limits (`ErrValidation`) on any node type, so
a sortation centre or a delivery station could declare carton limits it will
never apply. The network model's gap register records both differences as gap
G64.

Two alternatives were weighed. Keeping the table as it was and letting
consumers special-case fulfillment centres: refused, the capabilities are
derived from the type here (`DefaultCapabilitiesByType`) precisely so no
consumer has to. Refusing stored profiles that carry limits on a node type
without `INBOUND` (a restore check, or a data migration that strips them):
refused, the stored revisions are immutable facts (ADR 0002) that were valid
when they were declared, and a restore that refuses them would turn a contract
tightening into an outage. Only new declarations are gated.

## Decision

The contract, verbatim:

1. typeRules: FULFILLMENT_CENTER capabilities = [OUTBOUND, INBOUND]. INBOUND_CROSS_DOCK stays [INBOUND] and RETURNS_CENTER stays [RETURNS]. Parcel node types (SORTATION_CENTER, DELIVERY_STATION, AIR_HUB) are unchanged. Keep any capability ordering the code already uses.
2. New domain error ErrInboundLimitsNotAllowed: declaring an operational profile (or any command that sets the `inbound` limits object) on a node whose type lacks INBOUND is refused. HTTP 422 with problem type `inbound-limits-not-allowed` (follow the repo's problem-type naming and catalogue; add it to apis/openapi.yaml responses and the problem-types list, regenerate the REST docs). Value checks from ADR 0007 still apply when the type has INBOUND.
3. Already-persisted profiles that carry inbound limits on a non-INBOUND node stay restorable (Restore must not refuse them). Only NEW declarations are refused. No data migration.
4. GET /nodes/{nodeId} (and list reads, and any MCP read tool exposing capabilities) report inbound:true for FULFILLMENT_CENTER. Regenerate the MCP golden if any output changes.
5. No event shape change. NodeRegistered/NodeOperationalProfileDeclared payloads stay as they are. If an event carries a capabilities list, its values for FCs now include INBOUND; say so in the ADR consequences.
6. ADR: the next free number (expected 0015), titled 'Fulfillment centres receive inbound'. Link the network model gap G64 in prose (no URL needed).
7. Tests: unit tests for the capability table and the refusal; HTTP handler test for 422 inbound-limits-not-allowed; repocontract unchanged unless the snapshot changes; e2e/integration only if they assert capabilities (update their expectations).

How it is built:

- `Capabilities` is a struct of booleans, so there is no list ordering to keep:
  the `FULFILLMENT_CENTER` row of `typeRules` sets `Inbound: true` beside
  `Outbound: true`, and `TestTypeRulesPinTheModel` pins the row.
- `node.ErrInboundLimitsNotAllowed` is raised by `Node.DeclareProfile` (the only
  command that sets `inbound`) when the declaration carries `inbound` and the
  node's type lacks `INBOUND`. The gate runs before the ADR 0007 value checks,
  so a node that may not declare limits learns that first, whatever the values.
  An explicit `"inbound": null` in a request is still read as absent (ADR 0007
  decision 5) and passes the gate.
- `Restore` does not call the gate: it keeps checking the stored limits' values
  only, as before.

## Consequences

- `GET /nodes`, `GET /nodes/{nodeId}`, the profile `PUT` response, the MCP
  `get_node` and `list_nodes` tools and the A2A skills `serving_station` and
  `find_nodes` now report `inbound: true` for every
  fulfillment centre, including the ones registered before this change: the
  capabilities are derived on read, never stored, so no data moves.
- No event changes shape. Neither `NodeRegistered` nor
  `NodeOperationalProfileDeclared` carries a capabilities list (ADR 0004:
  consumers derive capabilities from `nodeType`). A consumer that derives them
  with the model's `DefaultCapabilitiesByType` now derives `INBOUND` for
  `FULFILLMENT_CENTER`, the value the model always had.
- A client that declared `inbound` on a sortation centre, delivery station, air
  hub or returns centre now gets `422 inbound-limits-not-allowed`; the remedy is
  to drop the object. Revisions already stored with such limits stay readable,
  and a later revision without `inbound` supersedes them for consumers.
- No new endpoint, topic, consumer, migration or environment variable; the chart
  is unchanged.
