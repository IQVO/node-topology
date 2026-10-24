# ADR 0008: a read-only MCP server

## Status

Accepted (2026-10-24).

## Context

Every other bounded context of the fleet that exposes an HTTP API also ships an
MCP server so that agents (warehouse-ops-agent first) can read its data without
a bespoke REST client. node-topology had none. The service already has the read
use cases behind its REST adapter (`GetNode`, `ListNodes`, `ListLanes`,
`ResolveServiceArea`, `GetRegion`, `ListRegions` — the Open Host Service reads
of ADR 0003), so an MCP adapter is another driving adapter over them, not new
domain.

## Decision

- **A second driving adapter, a separate binary.** `internal/adapters/inbound/mcp`
  is built on the official MCP Go SDK (`github.com/modelcontextprotocol/go-sdk`
  v1.8.0) and served over Streamable HTTP only (no stdio, no SSE) by `cmd/mcp`,
  mounted at `/mcp` and `/` with an open `/healthz`. It is a second deployable
  beside `cmd/api`; the Helm chart gets an mcp Deployment and Service (default
  off) and an optional gateway HTTPRoute at `/api/mcp/node-topology`.
- **Six read tools over the existing queries, nothing else — one per read GET
  of apis/openapi.yaml.** `get_node(nodeId)`, `list_nodes(type?, regionId?,
  status?)`, `list_lanes(origin?, destination?)`, `resolve_service_area(postalPrefix)`,
  `get_region(regionId)` and `list_regions()`. Arguments and results are
  lowerCamelCase like the REST contract (`list_regions` returns its array under
  a `regions` key so a tool's structured content is always an object); typed
  errors carry the REST problem slug (`not-found`, `validation-failed`) as the
  prefix of the tool error. Registering a node, declaring a profile revision,
  the lifecycle transitions, opening/closing lanes, assigning service areas and
  defining regions stay REST-only.
- **Read-only is structural.** The adapter's `Deps` holds the six read use
  cases and nothing that can write; `cmd/mcp` imports no event sink, no outbox
  writer, no Kafka adapter and never dials Kafka (this service consumes no
  topic anyway): the relay runs in `cmd/api` only. `TestToolSurface` pins the
  exact tool set (budget 6), the read-only annotations and the absence of write
  verbs in tool names; `TestToolRegistryGolden` pins the whole advertised
  registry; the integration test proves the binary reads what `cmd/api`'s use
  cases wrote in a real Postgres and adds no outbox row. Optional tool inputs
  carry `,omitempty` so the SDK publishes them as optional
  (`internal/architecture/mcp_optional_inputs_test.go`).
- **No sibling calls, no auth.** The server reads this context's own tables
  only, calls no other context over REST or MCP, and has no auth layer (the
  fleet-wide revert of 2026-09-11). Access control is the in-cluster ClusterIP
  boundary.
- **Boot.** `DATABASE_URL` is mandatory (no in-memory mode); the binary runs
  the idempotent embedded migrations on start (with `MIGRATIONS_DATABASE_URL`
  for a direct DSN behind PgBouncer), wrapped in the same `bootretry` budget as
  `cmd/api`, so it can start against a fresh database before `cmd/api` has.
  `MCP_ADDR` (default `:8090`), `LOG_LEVEL`, `SERVICE_VERSION` and
  `ENVIRONMENT` complete the environment.

## Consequences

- Agents can answer "which nodes are ACTIVE in region X", "what profile does
  this fulfilment centre hold", "which station serves postal prefix 013"
  without REST glue.
- Adding a tool is a reviewed act: the tool budget, the curated list and the
  golden file all change together (an amendment of this ADR).
- Writes stay REST operations with the `Idempotency-Key` middleware and the
  transactional outbox; the MCP surface cannot reach them at all.
