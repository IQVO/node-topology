# node-topology

Retail network, bounded context **Node Topology** (Supporting, logistics tier): the authoritative registry of the network's nodes, their versioned
operational profiles, lanes, regions and postal service areas.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

The design is proposed in [retail-network docs](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology); `model/model.yaml` there is the single source of truth.
This repository is a service of the retail network, not of the warehouse-systems fleet; the fleet reaches the network only through the
`vendor-connectivity` Vendor API.

## What it does

- **Nodes**: register a node (fulfillment centre, cross-dock, returns centre, sortation centre, delivery station, air hub), declare its operational
  profile (a new immutable revision each time; optionally with the inbound limits the node accepts: carton weight in grams, carton size in
  millimetres, pallets, SSCC, [ADR 0007](docs/adr/0007-inbound-limits-on-the-operational-profile.md)), activate, suspend and retire it.
- **Lanes**: open and close directed GROUND / AIR connections between ACTIVE nodes.
- **Service areas**: assign a postal prefix to an ACTIVE delivery station.
- **Regions**: define and read the partitions of the network.
- Every change publishes a CloudEvents 1.0 event through a transactional outbox. The service consumes no Kafka topic and calls no other service.

Aggregates, invariants and every assumption the model left open: [docs/adr/0002](docs/adr/0002-aggregates-and-invariants.md).

## REST API

Contract: [`apis/openapi.yaml`](apis/openapi.yaml). No authentication. Errors are RFC 9457 `application/problem+json` (`type` is a slug such as
`not-found`, `illegal-transition`, `concurrent-update`, `validation-failed`, `stale-profile-revision`).
Every state-changing request needs an `Idempotency-Key` header: a repeat with the same request replays the first response for 24 hours.

| Method and path | Operation |
| --- | --- |
| `POST /nodes`, `GET /nodes?type=&region=&status=`, `GET /nodes/{nodeId}` | register, list, read |
| `PUT /nodes/{nodeId}/operational-profile` | declare a profile revision |
| `POST /nodes/{nodeId}:activate`, `:suspend`, `:retire` | lifecycle |
| `POST /lanes`, `GET /lanes?origin=&destination=`, `POST /lanes/{laneId}:close` | lanes |
| `PUT /service-areas/{postalPrefix}`, `GET /service-areas/{postalPrefix}` | assign, resolve |
| `POST /regions`, `GET /regions`, `GET /regions/{regionId}` | regions |
| `GET /healthz`, `GET /readyz` | liveness, readiness |

## MCP server

[ADR 0008](docs/adr/0008-mcp-server-adoption.md). `cmd/mcp` is a second, read-only deployable over the SAME read use cases as the REST adapter, served over
Streamable HTTP (`/mcp`, official Go SDK) with an open `/healthz`. No auth (fleet decision), no Kafka, no outbox — it can never write. Tools: `get_node`,
`list_nodes(type?, regionId?, status?)`, `list_lanes(origin?, destination?)`, `resolve_service_area(postalPrefix)`, `get_region`, `list_regions`; tool errors
carry the REST problem slug (`not-found`, `validation-failed`).

```sh
export DATABASE_URL=postgres://user:***@localhost:5432/node_topology?sslmode=disable
go run ./cmd/mcp                     # listens on :8090 (MCP_ADDR)
```

## Events

Contract: [`apis/asyncapi.yaml`](apis/asyncapi.yaml), catalogue in [ADR 0004](docs/adr/0004-cloudevents-envelope-and-type-catalogue.md).
All nine events go to `retailnetwork.node-topology.events`, CloudEvents 1.0 structured JSON, type
`com.retailnetwork.logistics.node-topology.<entity>.<Event>` (`node`, `lane`, `servicearea`, `region`), `subject` and Kafka key = aggregate id:
`NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`, `NodeSuspended`, `NodeRetired`, `LaneOpened`, `LaneClosed`,
`ServiceAreaAssigned`, `RegionDefined`. Delivery is at-least-once with a stable event id ([ADR 0005](docs/adr/0005-outbox-relay-and-attempt-cap.md)).

## Run it

Real PostgreSQL and Kafka only. `DATABASE_URL` and `KAFKA_BROKERS` are mandatory (a boot error names the missing variable); the embedded migrations
run at boot and the outbox relay is always on.

```sh
export DATABASE_URL=postgres://user:pass@localhost:5432/node_topology?sslmode=disable
export KAFKA_BROKERS=localhost:29092   # whatever your broker advertises
go run ./cmd/api                       # listens on :8080 (HTTP_ADDR)
```

All settings, the probes, the shutdown order and the outbox runbook: [docs/runtime.md](docs/runtime.md).

## Deploy

`charts/node-topology/` deploys the api alone (Deployment, Service, ConfigMap, Secret or `database.existingSecret`, ServiceAccount; HPA, HTTPRoute and
Ingress are off by default). `database.url` or `database.existingSecret`, and `kafka.brokers`, are mandatory: the chart refuses to render without them.
This service consumes no topic, so there is no consumer group value.

```sh
helm lint charts/node-topology -f charts/node-topology/ci/default-values.yaml
helm template node-topology charts/node-topology -f charts/node-topology/ci/default-values.yaml
bash charts/node-topology/tests/render_test.sh && python3 charts/node-topology/tests/test_service_selectors.py   # needs helm and PyYAML
```

`mcp.enabled=true` adds the read-only MCP Deployment + Service (and `mcp.route.enabled=true` the gateway route `/api/mcp/node-topology` -> `:8090`); it needs
`database.*` but no brokers. `frontend.enabled=true` adds the web remote's nginx Deployment + ClusterIP Service; it needs no database and no Kafka, and this
chart never routes it (the web gateway owns `/mfes/node-topology/`).

## Observability

Logs, metrics and traces follow the fleet standard ([ADR 0010](docs/adr/0010-fleet-observability-standard.md)): JSON logs on stdout carry `trace_id`/`span_id`
and a `request_id`; `cmd/api` and `cmd/mcp` export OTLP/gRPC to the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `SERVICE_VERSION`, `ENVIRONMENT`;
export never blocks, so no Collector costs telemetry, nothing else) with HTTP request duration, Go runtime metrics and the business counter
`topology.nodes.registered` (`outcome`, `reason`); the trace continues across Kafka in the `traceparent` header of each published event. The chart's `otel.*`
values render the env.

## Web remote

[ADR 0009](docs/adr/0009-console-remote.md). `web/` is the `node_topology_mfe` Module Federation remote the warehouse-console shell mounts under
`/node-topology/*` (federation name `node_topology_mfe`, exposes `./App`, dev/preview port 4017, build base `/mfes/node-topology/`). Three read-only screens —
the node registry (type/region/status filters, node detail with its operational profile), the lanes browser and the regions browser — over this context's own
GET routes; `apiOrigin` comes from the shell's `/config.json` at runtime. The image is the two-stage uikit named-context build
(`docker build --build-context uikit=../../warehouse-ui-kit -t ghcr.io/iqvo/node-topology-frontend:local .` from `web/`).

```sh
cd web && npm ci && npm run lint && npx tsc -b && npm test && npm run build
```

## Develop

| Command | What it runs |
| --- | --- |
| `make check` | format, vet, build, lint, unit, handler and BDD tests (the pre-push gate) |
| `make coverage` | 90% gate over `internal/domain` and `internal/application` |
| `make mutation` | gremlins on every `internal/domain/<pkg>` |
| `make arch-test` | hexagonal and harness fitness tests |
| `make integration` | testcontainers Postgres and Kafka: repositories, relay, middleware, real-wiring test (needs Docker) |
| `make vuln` | govulncheck |

Layout: `internal/domain/{shared,node,lane,servicearea,region}` (pure Go), `internal/application/{ports,views,usecases,repository,outbox}`,
`internal/adapters/inbound/{http,mcp}` (chi; MCP Go SDK), `internal/adapters/outbound/{postgres,kafka,outbox,clock,ids,memory,repocontract}`, `cmd/{api,mcp}`, `web/` (the node_topology_mfe Module Federation remote),
`features/` (godog BDD). The `memory` and `repocontract` packages are test doubles; `cmd/**` never imports them.

Decisions: [docs/adr/](docs/adr/index.md). Packaging checklist: [docs/before-first-release.md](docs/before-first-release.md).
