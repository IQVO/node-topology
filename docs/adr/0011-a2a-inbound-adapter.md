# ADR 0011: A2A inbound adapter

## Status

Accepted (2026-10-29).

## Context

The fleet has three transports with no overlap: Kafka + CloudEvents carries
FACTS, MCP carries an agent's TOOLS, and Agent2Agent (A2A) carries the
DELEGATION of a goal between agents, never data sync. node-topology already has
the first two (the outbox relay of [ADR 0005](./0005-outbox-relay-and-attempt-cap.md)
and the read-only MCP server of [ADR 0008](./0008-mcp-server-adoption.md)); it
had no way for a peer agent to hand it a goal ("which station delivers to
013, and when does it operate?") instead of choreographing six tool calls.

[ADR 0003](./0003-no-consumed-contracts-and-ohs-reads.md) makes this context an
Open Host Service for reads: it consumes no topic and calls no sibling. A2A is
one more driving adapter over those same reads; it does not change that
direction.

## Decision

- **A third driving adapter, a separate binary.**
  `internal/adapters/inbound/a2a` is built on `github.com/a2aproject/a2a-go/v2`
  (pinned v2.7.0) and served by `cmd/a2a` over JSON-RPC 2.0 on HTTP: the Agent
  Card at `/.well-known/agent-card.json`, the JSON-RPC endpoint at BOTH `/` and
  `/a2a`, an open `/healthz`. `cmd/a2a` is cloned from `cmd/mcp` (same
  composition, same Postgres repositories, same boot migrations behind
  `bootretry`, same observability standard of [ADR 0010](./0010-fleet-observability-standard.md))
  and listens on `A2A_ADDR` (default `:8093`; 8080 is the api, 8090 the mcp
  server). `A2A_AGENT_URL`, the absolute http(s) URL advertised on the card, is
  MANDATORY: the binary refuses to boot, before touching the database or
  binding a listener, when it is unset, relative or not http(s).
- **A message-only agent with two curated, goal-shaped READ skills, not one per
  endpoint.** The input contract is `{"skill":"<id>","arguments":{...}}` as a
  data part (or a text part holding the same JSON); the answer is one agent
  message with a data part (the envelope `{skill, data | problem}`) and a text
  part (a one-line summary), bound to the caller's context id.
  - `serving_station(postalPrefix)`: resolves the delivery station serving a
    postal prefix and returns the assignment (`deliveryStationNodeId`,
    `regionId`, `assignmentVersion`) together with that station's node and
    CURRENT operational profile (operating hours, holidays, handling time,
    cutoffs, throughput cap). A composition of `ResolveServiceArea` and
    `GetNode`; MCP-derived (`resolve_service_area` + `get_node`).
  - `find_nodes(type?, regionId?, status?)`: the nodes matching the filters
    (AND), by id ascending, each with its profile revision (the full profile is
    `serving_station`'s job, so a listing stays small). The `ListNodes` use case;
    MCP-derived (`list_nodes`).
  Lanes, regions, single-node reads and the whole write side are not skills:
  they stay MCP tools and REST operations.
- **The write gate: the agent is READ-ONLY.** A skill may change state only if
  the context's MCP surface already offers that write, and the MCP charter
  ([ADR 0008](./0008-mcp-server-adoption.md), `governance_test.go`) is
  read-only. So `Deps` holds three read use cases and nothing that can write,
  `cmd/a2a` wires no write use case, no outbox writer, no publisher and no Kafka
  adapter and never starts the relay (it consumes no topic anyway), so this
  binary can never append to the outbox. `internal/architecture/a2a_fitness_test.go`
  pins it (every `Deps` field is a `Get*`/`List*`/`Resolve*` use case); the
  wire tests pin that write-shaped skill ids (`register_node`,
  `assign_service_area`, `suspend_node`) are answered as unknown skills and that
  no store row, outbox row or aggregate version moves through any skill; the
  integration test proves the same against real Postgres. Because there is no
  write skill there is no delegated-write outbox proof to run: facts keep
  leaving exclusively from REST through the api's transactional outbox and
  relay.
- **Strict input.** Requests decode with `DisallowUnknownFields` and reject
  trailing data, so a misspelt filter (`statuz`) can never silently widen a
  query to every node; absent arguments are the empty object and each skill's
  own required-argument check answers. A U+0000 in any string argument is
  refused as an invalid argument (Postgres cannot compare it; REST answers 400
  for it too).
- **Problems are ANSWERED, never raised.** Domain rejections and bad requests
  travel in the message envelope as RFC 9457-shaped problems (`type` is the
  stable slug, like the REST adapter's `type`), never as JSON-RPC errors, with
  the REST adapter's status classification
  (`internal/adapters/inbound/http/problems.go`): `not-found` 404 (unassigned
  prefix), `validation-failed` 422 (a type or status that is not an enumeration
  value), adapter-owned `malformed-skill-request`, `unknown-skill` and
  `invalid-argument` 400 (REST's malformed-body class), and `internal-error`
  500 whose detail never carries the underlying error text (it is logged).
  Only the errors a READ can raise are mapped, as in the MCP adapter; the 409
  and 503 classes of REST need a write or a readiness probe and cannot occur
  here. No shared domain error text in this context names a deprecated alias, so
  no adapter-owned detail rewrite was needed.
- **Observability.** One OTel span per skill (`a2a.skill <id>`, attribute
  `a2a.skill`), mirroring `mcp.tool`, under the server span and the request log
  of [ADR 0010](./0010-fleet-observability-standard.md); the binary exports as
  `node-topology-a2a`.
- **No auth, no outbound.** The surface is unauthenticated like REST and MCP
  (fleet-wide revert of 2026-09-11); the card declares no security scheme.
  Access control is the in-cluster ClusterIP boundary. No outbound A2A client
  exists in this repository: the context calls no sibling, and
  `a2a_fitness_test.go` fails the build on any non-test import of the SDK's
  `a2aclient` package or of the SDK outside the adapter and `cmd/a2a`.
- **Helm.** `a2a-deployment`, `a2a-service` and `a2a-route` mirror the mcp
  templates (component `a2a`, command `/app/a2a`, port 8093, the `a2a:` values
  block DEFAULT OFF, helper `node-topology.a2aFullname`). `A2A_AGENT_URL`
  defaults to `http://<release>-a2a.<namespace>.svc.cluster.local:8093/` and is
  overridable with `a2a.agentUrl`; the optional gateway route
  `/api/a2a/node-topology` rewrites to `/`, so the card stays reachable under
  the same prefix. The Deployment gets the database and observability env and
  NO `KAFKA_BROKERS`, `EVENT_PUBLISHER` or relay env. The render test derives
  the mandatory variables from the `"<VAR> is required"` boot errors of
  `cmd/a2a`; the selector test pins that each Service selects exactly one
  Deployment. The Dockerfile already builds every `cmd/*` binary into `/app`.

## Consequences

- A peer agent can ask the goal-shaped questions in one message instead of
  composing MCP tool calls; the answers come from this context's own tables
  through the same use cases as REST and MCP (one action surface, three
  protocols).
- Adding a skill is a reviewed act (an amendment of this ADR): the card test,
  the skill table and the `Deps` fitness test change together. A write skill
  additionally needs the MCP charter to offer that write first and an
  integration test proving the delegated write leaves UNPUBLISHED outbox rows.
- A second read surface to keep consistent with the REST/MCP views; the DTOs are
  adapter-owned and mapped from the application views, so domain structs never
  cross the boundary.
- One more Deployment per environment when `a2a.enabled=true`; the environment
  that deploys it supplies the gateway parentRefs (and may override
  `a2a.agentUrl` with the gateway path for off-cluster callers).
