# Node Topology

[![CI](https://github.com/IQVO/node-topology/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/IQVO/node-topology/actions/workflows/ci.yml?query=branch%3Adevelop) [![coverage gate](https://img.shields.io/badge/coverage%20gate-90%25-brightgreen)](https://github.com/IQVO/node-topology/blob/develop/Makefile) [![Go](https://img.shields.io/badge/go-1.27.3-00ADD8?logo=go&logoColor=white)](https://github.com/IQVO/node-topology/blob/develop/go.mod) [![release](https://img.shields.io/github/v/release/IQVO/node-topology?sort=semver)](https://github.com/IQVO/node-topology/releases) [![docs](https://img.shields.io/badge/docs-iqvo.github.io-blue)](https://iqvo.github.io/node-topology/) [![license](https://img.shields.io/badge/license-MIT-blue)](https://github.com/IQVO/node-topology/blob/develop/LICENSE)

Retail network, bounded context **Node Topology** (Supporting Subdomain,
logistics tier): the authoritative registry of the network's nodes, their
versioned operational profiles, lanes, regions and postal service areas. It
owns the topology facts every other network context reads — and nothing else:
it consumes no Kafka topic and calls no other service (an **Open Host
Service** with its REST reads and its Published Language of nine events,
ADR 0003). Its events are consumed by `delivery-promise`, `order-sourcing`,
`vendor-connectivity` and `node-performance`, all Conformists to the profile,
region and service-area revisions. This repository is a service of the retail
network, not of the warehouse-systems fleet; the fleet reaches the network
only through the `vendor-connectivity` Vendor API.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

## Documentation

The context is documented on the fleet-wide warehouse-docs site, in the
retail-network section: **<https://iqvo.github.io/warehouse-docs/network/contexts/node-topology>**
(the model under `network/model/model.yaml` in IQVO/warehouse-docs is the
single source of truth). Fleet-wide network pages:
[strategic design](https://iqvo.github.io/warehouse-docs/network/strategic-design/subdomain-classification),
[the CloudEvents event standard](https://iqvo.github.io/warehouse-docs/network/strategic-design/event-standard-cloudevents),
the [node model](https://iqvo.github.io/warehouse-docs/network/network-model/node-model),
and the [WMS/WES integration seam](https://iqvo.github.io/warehouse-docs/network/integration/wms-wes-integration).
This repository's own references: [`docs/runtime.md`](docs/runtime.md) (all
settings, the probes, the shutdown order and the outbox runbook) and the
packaging checklist [`docs/before-first-release.md`](docs/before-first-release.md).

Page map (on the warehouse-docs site, under `network/contexts/node-topology/`):

| Area | Pages |
|---|---|
| Business context | [Business context](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/business-context), [Use cases](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/use-cases) |
| Language & model | [Ubiquitous language](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/ubiquitous-language), [Domain events](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/domain-events), [Domain message flow](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/domain-message-flow), [Class diagram](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/class-diagram), [Sequence diagrams](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/sequence-diagrams), [Entity-relationship](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/entity-relationship) |
| DDD canvases | [Core domain chart](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/core-domain-chart), [Bounded context canvas](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/bounded-context-canvas), [Aggregate design canvas](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/aggregate-design-canvas), [EventStorming](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/eventstorming), [Context map](https://iqvo.github.io/warehouse-docs/network/contexts/node-topology/context-map) |

## What it does

- **Nodes**: register a node (fulfillment centre, cross-dock, returns centre, sortation centre, delivery station, air hub), declare its operational
  profile (a new immutable revision each time; optionally with the inbound limits the node accepts: carton weight in grams, carton size in
  millimetres, pallets, SSCC, [ADR 0007](docs/adr/0007-inbound-limits-on-the-operational-profile.md)), activate, suspend and retire it.
- **Lanes**: open and close directed GROUND / AIR connections between ACTIVE nodes.
- **Service areas**: assign a postal prefix to an ACTIVE delivery station.
- **Regions**: define and read the partitions of the network.
- Every change publishes a CloudEvents 1.0 event through a transactional outbox. The service consumes no Kafka topic and calls no other service.

Aggregates, invariants and every assumption the model left open: [docs/adr/0002](docs/adr/0002-aggregates-and-invariants.md).

## Architecture

Hexagonal / ports & adapters; dependencies point inward only:

```
cmd/api/                      composition root: the REST adapter and the outbox relay
cmd/mcp/                      read-only MCP server (Streamable HTTP, ADR 0008)
cmd/a2a/                      read-only A2A agent (JSON-RPC over HTTP, ADR 0011)
internal/
  domain/                     pure Go — shared, node, lane, servicearea, region
  application/                ports, views, usecases, repository, outbox
  adapters/
    inbound/http/             chi handlers, problem+json mapping, readiness
    inbound/mcp/              MCP tools (read-only)
    inbound/a2a/              A2A skills (read-only)
    inbound/requestlog/       request logging middleware
    kafka/cloudevents/        CloudEvents 1.0 codec
    outbound/postgres/        repos, embedded migrations, pgtx UnitOfWork
    outbound/outbox/          transactional outbox + relay with attempt cap
    outbound/kafka/           the topic publisher
    outbound/clock/, ids/     the wall clock and id source
    outbound/memory/, repocontract/   test doubles; cmd/** never imports them
  architecture/               the fitness tests
  bootretry/                  bounded boot retry
web/                          the node_topology_mfe Module Federation remote
features/                     godog BDD
```

The layering is enforced as an executable test, not a convention:
`internal/architecture` (`make arch-test`) fails the build if the domain ever
imports an outer layer or an adapter reaches sideways into its sibling.

## Domain events

Contract: [`apis/asyncapi.yaml`](apis/asyncapi.yaml), catalogue in [ADR 0004](docs/adr/0004-cloudevents-envelope-and-type-catalogue.md).
All nine events go to `retailnetwork.node-topology.events`, CloudEvents 1.0
structured JSON (`source` `/retailnetwork/node-topology`, `dataschema`
`urn:retailnetwork:node-topology:events:<EventName>:v1`), type
`com.retailnetwork.logistics.node-topology.<entity>.<Event>` (`node`, `lane`,
`servicearea`, `region`), `subject` and Kafka key = aggregate id:
`NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`,
`NodeSuspended`, `NodeRetired`, `LaneOpened`, `LaneClosed`,
`ServiceAreaAssigned`, `RegionDefined`. Delivery is at-least-once with a
stable event id ([ADR 0005](docs/adr/0005-outbox-relay-and-attempt-cap.md)).
Nothing is consumed (ADR 0003); downstream consumers include
`delivery-promise`, `order-sourcing`, `vendor-connectivity` and
`node-performance`.

## Running it

Real PostgreSQL and Kafka only. `DATABASE_URL` and `KAFKA_BROKERS` are mandatory (a boot error names the missing variable); the embedded migrations
run at boot and the outbox relay is always on.

```sh
export DATABASE_URL=postgres://user:***@localhost:5432/node_topology?sslmode=disable
export KAFKA_BROKERS=localhost:29092   # whatever your broker advertises
go run ./cmd/api                       # listens on :8080 (HTTP_ADDR)
```

### Configuration

| Env var | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | **required** | Postgres DSN of the service's pool |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | Direct DSN for the boot migrations (golang-migrate holds a session advisory lock, which breaks behind a transaction-pooling proxy) |
| `KAFKA_BROKERS` | **required** | Comma-separated `host:port` list (relay and `/readyz`) |
| `HTTP_ADDR` | `:8080` | Listen address |
| `OUTBOX_RELAY_INTERVAL` | `1s` | Poll interval of the relay (Go duration or seconds) |
| `OUTBOX_MAX_ATTEMPTS` | `20` | Failures before the relay excludes a row; `0` = unlimited (ADR 0005) |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | Wait between flipping `/readyz` to 503 and closing the listener; `0` disables |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `OTEL_SERVICE_NAME` | `node-topology` | `service.name` (`node-topology-mcp` for `cmd/mcp`, `node-topology-a2a` for `cmd/a2a`; ADR 0010) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | Collector OTLP/gRPC; export never blocks |
| `SERVICE_VERSION` / `ENVIRONMENT` | `dev` / `local` | `service.version` / `deployment.environment.name` |

`cmd/mcp` adds `MCP_ADDR` (`:8090`); `cmd/a2a` adds `A2A_ADDR` (`:8093`) and
the mandatory `A2A_AGENT_URL`. Both read only `DATABASE_URL` (plus the
optional `MIGRATIONS_DATABASE_URL`) and the telemetry/log variables — no
Kafka, no outbox.

## Observability

Logs, metrics and traces follow the fleet standard ([ADR 0010](docs/adr/0010-fleet-observability-standard.md)): JSON logs on stdout carry `trace_id`/`span_id`
and a `request_id`; `cmd/api`, `cmd/mcp` and `cmd/a2a` export OTLP/gRPC to the Collector (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `SERVICE_VERSION`, `ENVIRONMENT`;
export never blocks, so no Collector costs telemetry, nothing else) with HTTP request duration, Go runtime metrics and the business counter
`topology.nodes.registered` (`outcome`, `reason`); the trace continues across Kafka in the `traceparent` header of each published event. The chart's `otel.*`
values render the env.

## API

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

### MCP server (read-only)

[ADR 0008](docs/adr/0008-mcp-server-adoption.md). `cmd/mcp` is a second, read-only deployable over the SAME read use cases as the REST adapter, served over
Streamable HTTP (`/mcp`, official Go SDK) with an open `/healthz`. No auth (fleet decision), no Kafka, no outbox — it can never write. Tools: `get_node`,
`list_nodes(type?, regionId?, status?)`, `list_lanes(origin?, destination?)`, `resolve_service_area(postalPrefix)`, `get_region`, `list_regions`; tool errors
carry the REST problem slug (`not-found`, `validation-failed`).

```sh
export DATABASE_URL=postgres://user:***@localhost:5432/node_topology?sslmode=disable
go run ./cmd/mcp                     # listens on :8090 (MCP_ADDR)
```

### A2A agent (read-only)

[ADR 0011](docs/adr/0011-a2a-inbound-adapter.md). `cmd/a2a` is a third, read-only deployable over the SAME read use cases as REST and MCP: an Agent2Agent
(JSON-RPC over HTTP, official Go SDK) message-only agent with the Agent Card at `/.well-known/agent-card.json`, JSON-RPC at `/` and `/a2a`, and an open `/healthz`.
A2A carries the DELEGATION of a goal, never data sync (facts stay CloudEvents on Kafka). No auth (fleet decision), no Kafka, no outbox, no outbound A2A client — it can
never write (the MCP charter is read-only, so there is no write skill). Two skills, sent as `{"skill": "<id>", "arguments": {...}}`: `serving_station(postalPrefix)`
(the delivery station serving a prefix, with its node and current operational profile) and `find_nodes(type?, regionId?, status?)`. Rejections are answered in the
message as RFC 9457-shaped problems with the REST status classification (`not-found` 404, `validation-failed` 422, `malformed-skill-request` / `unknown-skill` /
`invalid-argument` 400, `internal-error` 500), never as JSON-RPC errors.

```sh
export DATABASE_URL=postgres://user:***@localhost:5432/node_topology?sslmode=disable
export A2A_AGENT_URL=http://localhost:8093/   # mandatory: the absolute URL advertised on the card
go run ./cmd/a2a                              # listens on :8093 (A2A_ADDR)
```

## Helm chart

`charts/node-topology/` deploys the api alone (Deployment, Service, ConfigMap, Secret or `database.existingSecret`, ServiceAccount; HPA, HTTPRoute and
Ingress are off by default). `database.url` or `database.existingSecret`, and `kafka.brokers`, are mandatory: the chart refuses to render without them.
This service consumes no topic, so there is no consumer group value.

```sh
helm lint charts/node-topology -f charts/node-topology/ci/default-values.yaml
helm template node-topology charts/node-topology -f charts/node-topology/ci/default-values.yaml
bash charts/node-topology/tests/render_test.sh && python3 charts/node-topology/tests/test_service_selectors.py   # needs helm and PyYAML
```

The CI `helm-lint` job runs `ct lint` over every `charts/node-topology/ci/*-values.yaml` plus the selector wiring test; `chart-tests` runs the render test.

### Running the MCP server in Kubernetes

`mcp.enabled=true` adds the read-only MCP Deployment + Service (and `mcp.route.enabled=true` the gateway route `/api/mcp/node-topology` -> `:8090`); it needs
`database.*` but no brokers. `a2a.enabled=true` adds the read-only A2A Deployment + Service (and `a2a.route.enabled=true` the gateway route `/api/a2/node-topology` -> `:8093`, prefix rewritten to `/`); `A2A_AGENT_URL` defaults to the in-cluster Service DNS (`a2a.agentUrl` overrides it), it needs `database.*` but no brokers. `frontend.enabled=true` adds the web remote's nginx Deployment + ClusterIP Service; it needs no database and no Kafka, and this
chart never routes it (the web gateway owns `/mfes/node-topology/`). Both extra surfaces are read-only and unauthenticated by fleet decision — the in-cluster
ClusterIP boundary is the access control.

## Operator micro-frontend (web/)

[ADR 0009](docs/adr/0009-console-remote.md). `web/` is the `node_topology_mfe` Module Federation remote the warehouse-console shell mounts under
`/node-topology/*` (federation name `node_topology_mfe`, exposes `./App`, dev/preview port 4017, build base `/mfes/node-topology/`). Three read-only screens —
the node registry (type/region/status filters, node detail with its operational profile), the lanes browser and the regions browser — over this context's own
GET routes; `apiOrigin` comes from the shell's `/config.json` at runtime. The image is the two-stage uikit named-context build
(`docker build --build-context uikit=../../warehouse-ui-kit -t ghcr.io/iqvo/node-topology-frontend:local .` from `web/`). `@warehouse/ui-kit` is a sibling
checkout, so run npm commands from `web/` with `warehouse-ui-kit` next to this repository.

```sh
cd web && npm ci && npm run lint && npx tsc -b && npm test && npm run build
```

## Local development / quality gate

| Command | What it runs |
| --- | --- |
| `make check` | format, vet, build, lint, unit, handler and BDD tests (the pre-push gate) |
| `make check-fast` | fmt-check, vet, arch-test + changed-package tests (Stop hook) |
| `make coverage` | 90% gate over `internal/domain` and `internal/application` |
| `make mutation` | gremlins on every `internal/domain/<pkg>` |
| `make arch-test` | hexagonal and harness fitness tests |
| `make integration` | testcontainers Postgres and Kafka: repositories, relay, middleware, real-wiring test (needs Docker) |
| `make vuln` | govulncheck |

lefthook (`lefthook install` once per clone) runs `fmt-check`, `vet`, `lint`
on pre-commit and `make check` on pre-push.

## Tests

- unit + handler tests: `make test` (`go test ./... -race`, CI job `test`,
  with the 90 % coverage gate in the same job)
- BDD acceptance: godog/Gherkin in `features/` (`make bdd`, CI job `bdd`)
- integration: testcontainers Postgres + Kafka — repositories, relay,
  middleware, the real-wiring test (`make integration`, CI job `integration`;
  needs only Docker)
- mutation: gremlins per `internal/domain` package — shared, region, node,
  lane, servicearea (CI job `mutation-fast`; the exhaustive `mutation` job is
  scheduled)
- architecture fitness: `internal/architecture` (CI job `arch-test`)
- API contract lint (CI job `api-lint`), spec/model drift (`drift`,
  scheduled), `lint`, `guide-lint`, `complexity`, `vuln`, `web` (the
  vitest/oxlint/tsc suite of `web/`), `helm-lint` + `chart-tests` (chart),
  `docker-publish` + `release` (main only)

## Architecture Decision Records

Index: [`docs/adr/index.md`](docs/adr/index.md) — 11 ADRs (0001–0011): the
bounded context (0001), aggregates and invariants (0002), no consumed
contracts and OHS reads (0003), the CloudEvents catalogue (0004), the outbox
relay and attempt cap (0005), the HTTP adapter, idempotency and readiness
(0006), inbound limits on the operational profile (0007), the MCP server
(0008), the console remote (0009), the fleet observability standard (0010),
the A2A inbound adapter (0011).

## License

[MIT](LICENSE).
