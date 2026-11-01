---
id: configuration
title: Configuration
sidebar_label: Configuration
description: "Every environment variable read by the three Node Topology binaries (api, mcp, a2a): default, required or not, and meaning."
---

# Configuration

Node Topology ships three independent binaries. Each reads its own environment; there are no config files and no flags. Defaults below come from `cmd/api/config.go`, `cmd/api/telemetry.go`, `cmd/mcp/main.go`, `cmd/a2a/main.go`, `internal/adapters/outbound/postgres/config.go` and `internal/adapters/outbound/telemetry/telemetry.go`.

| Binary | Role | Default port | Reads Kafka? |
| --- | --- | --- | --- |
| `cmd/api` | REST API, migrations, outbox relay | `:8080` | yes (relay + `/readyz`) |
| `cmd/mcp` | read-only MCP server (Streamable HTTP) | `:8090` | no |
| `cmd/a2a` | read-only A2A agent (JSON-RPC) | `:8093` | no |

All three run on real PostgreSQL; there is no in-memory runtime. A missing mandatory variable is a boot error that names the variable and is raised before any listener binds. Probe, shutdown and outbox semantics are on [Runtime](/docs/overview/runtime) and the [runbook](/docs/operations/runbook).

## `cmd/api`

| Variable | Default | Required | Meaning |
| --- | --- | --- | --- |
| `DATABASE_URL` | none | **yes** | Postgres DSN of the service pool. Boot error `missing required environment variable: DATABASE_URL` when unset or empty. |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct DSN used only for the embedded boot migrations. golang-migrate takes a session advisory lock, which breaks behind a transaction-pooling proxy such as PgBouncer. |
| `KAFKA_BROKERS` | none | **yes** | Comma-separated `host:port` list used by the outbox relay and the `/readyz` metadata check. Entries are trimmed and blanks dropped; an empty result is a boot error naming the variable. |
| `HTTP_ADDR` | `:8080` | no | Listen address of the REST API. |
| `OUTBOX_RELAY_INTERVAL` | `1s` | no | Poll interval of the outbox relay: a Go duration (`500ms`) or plain seconds (`2`). Malformed or non-positive values log a warning and use the default. |
| `OUTBOX_MAX_ATTEMPTS` | `20` | no | Failures after which the relay stops claiming a row. `0` means unlimited; negative or unparsable values log a warning and use `20` (ADR 0005). |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | no | Wait between flipping `/readyz` to 503 and closing the listener (Go duration). `0` disables it; negative or unparsable values log a warning and use `5s`. |
| `LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` (or `warning`), `error`, case-insensitive. Anything else is `info`. |
| `OTEL_SERVICE_NAME` | `node-topology` | no | `service.name` on spans and metrics (and the Loki `app` label). |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint, bare `host:port` (becomes `http://host:port`) or a full URL. Export never blocks start-up. |
| `SERVICE_VERSION` | `dev` | no | `service.version` resource attribute. |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name` resource attribute. |

## `cmd/mcp`

`cmd/mcp` runs the same embedded migrations as the api (idempotent, guarded by the advisory lock) so it can boot on a fresh database, but it never starts the relay and never dials Kafka: there is no `KAFKA_BROKERS` here.

| Variable | Default | Required | Meaning |
| --- | --- | --- | --- |
| `MCP_ADDR` | `:8090` | no | Listen address of the Streamable HTTP MCP endpoint. |
| `DATABASE_URL` | none | **yes** | Postgres DSN. Boot error `DATABASE_URL is required: this context runs on real Postgres` when empty. |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct DSN for the boot migrations only. |
| `LOG_LEVEL` | `info` | no | As for the api. |
| `OTEL_SERVICE_NAME` | `node-topology-mcp` | no | `service.name` on spans and metrics. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint. |
| `SERVICE_VERSION` | `dev` | no | `service.version` (also in the boot log line). |
| `ENVIRONMENT` | `local` in telemetry, `dev` in the boot log line | no | `deployment.environment.name`. The two defaults differ in the code (`telemetry.DefaultEnvironment` versus the `getenv` fallback of the boot line); set it explicitly to get one value. |

## `cmd/a2a`

| Variable | Default | Required | Meaning |
| --- | --- | --- | --- |
| `A2A_ADDR` | `:8093` | no | Listen address of the JSON-RPC server (`/`, `/a2a`, Agent Card). |
| `A2A_AGENT_URL` | none | **yes** | Absolute `http` or `https` URL (with a host) that clients dial; advertised on the Agent Card. Checked first, before anything else starts: unset gives `A2A_AGENT_URL is required...`, a relative or non-http URL gives `A2A_AGENT_URL must be an absolute http(s) URL with a host`. |
| `DATABASE_URL` | none | **yes** | Postgres DSN. Same boot error as `cmd/mcp`. |
| `MIGRATIONS_DATABASE_URL` | value of `DATABASE_URL` | no | Direct DSN for the boot migrations only. |
| `LOG_LEVEL` | `info` | no | As for the api. |
| `OTEL_SERVICE_NAME` | `node-topology-a2a` | no | `service.name` on spans and metrics. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | Collector OTLP/gRPC endpoint. |
| `SERVICE_VERSION` | `dev` | no | `service.version`, also the version advertised on the Agent Card. |
| `ENVIRONMENT` | `local` in telemetry, `dev` in the boot log line | no | `deployment.environment.name`, same split as `cmd/mcp`. |

## Helm values

`charts/node-topology` maps the api variables to `config.*` values (`config.httpAddr`, `config.logLevel`, `config.outboxRelayInterval`, `config.outboxMaxAttempts`, `config.shutdownDrainDelay`), `kafka.brokers`, the `database.*` Secret references, and the `otel.*` and `environment` values. The mcp and a2a components get `-mcp` and `-a2a` service names. Details are on the [runbook](/docs/operations/runbook#deployment-helm).

## Not configurable

- Request-body and field limits on the operational profile are constants (ADR 0007), not variables.
- The Kafka topic (`retailnetwork.node-topology.events`) is a constant; the service consumes no topic, so there is no consumer-group variable.
- REST, MCP and A2A are unauthenticated: there are no key or token variables.
