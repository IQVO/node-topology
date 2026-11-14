---
id: runbook
title: Runbook
sidebar_label: Runbook
description: "Operating Node Topology: deployment shape, probes, migrations, the outbox relay, failure modes and routine procedures."
---

# Node Topology runbook

Node Topology is the retail network's registry bounded context (a domain-driven design study; see the disclaimer in the repository `README.md`): the authoritative source of the network's nodes (fulfillment centres, cross-docks, returns centres, sortation centres, delivery stations, air hubs), their immutable versioned operational profiles, the directed lanes between ACTIVE nodes, the regions, and which delivery station serves which postal prefix. It sits at the root of the network's dependency graph: every change is published as a CloudEvents 1.0 event on `retailnetwork.node-topology.events` through a transactional outbox, it consumes no Kafka topic and calls no sibling context ([ADR 0001](/docs/adr/0001-node-topology-bounded-context), [ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads)). Variables are in [Configuration](/docs/operations/configuration); probes and shutdown are described developer-side in [Runtime](/docs/overview/runtime). This page is the operator's companion.

## Runtime shape

Three binaries are built into one image (`Dockerfile` loops over `cmd/*/`; `ENTRYPOINT ["./api"]`, the other two are started with `command: ["/app/mcp"]` and `command: ["/app/a2a"]`). The chart deploys the api always; the MCP and A2A components are off by default (`mcp.enabled`, `a2a.enabled`) and switched on in the fleet environment (`warehouse-infra/helm-values/node-topology.yaml`).

| Component | Binary | Port | Does | Needs |
| --- | --- | --- | --- | --- |
| api | `cmd/api` | `:8080` | REST, migrations, outbox relay | Postgres, Kafka |
| mcp | `cmd/mcp` | `:8090` | read-only MCP over Streamable HTTP at `/mcp`, open `/healthz` | Postgres only |
| a2a | `cmd/a2a` | `:8093` | read-only A2A JSON-RPC at `/` and `/a2a`, Agent Card, open `/healthz` | Postgres only |

REST, MCP and A2A are unauthenticated by fleet decision; reachability is the cluster and gateway boundary.

- **REST** (`internal/adapters/inbound/http/server.go`): the 16 operations of `apis/openapi.yaml` on a chi router plus `/healthz` and `/readyz`; every state-changing route sits behind the `Idempotency-Key` middleware (24 h replay window, table `idempotency_keys`). Errors are RFC 9457 problems.
- **The outbox relay** (`internal/adapters/outbound/outbox/relay.go`, started in `cmd/api/relay.go`): the always-on loop that drains unpublished `outbox_events` rows to Kafka every `OUTBOX_RELAY_INTERVAL` (batch 100, one send at a time in `id` order, `FOR UPDATE SKIP LOCKED` claims in `internal/adapters/outbound/postgres/outbox_drain.go`). A full batch is followed immediately by another pass. Only `cmd/api` runs it; `cmd/mcp` and `cmd/a2a` have no write use case wired and cannot append to the outbox.
- **No consumers.** There is no Kafka consumer, no consumer group, no inbox table and no `*_CONSUMER_GROUP` variable: the service is the root producer of the network's shared event streams.

The relay is a worker: if it returns on its own, the api exits non-zero so Kubernetes restarts it (`cmd/api/run.go`, `serveUntilSignal`).

## Dependencies

- **Postgres.** Every write is one transaction (aggregates, outbox rows and the idempotency key together). `DATABASE_URL` is the pooled DSN (PgBouncer transaction pooling in the fleet); `MIGRATIONS_DATABASE_URL` is a direct, session-mode DSN used only for the boot migrations, because golang-migrate's session advisory lock cannot survive transaction pooling. Both come from the Secret `node-topology-db` through `secretKeyRef`; the migrations key is `optional: true` and the binary falls back to `DATABASE_URL`.
- **Kafka.** One cluster, outbound only, from the api. The relay publishes to `retailnetwork.node-topology.events`. The topic is auto-created by the first writer, so no topic exists until the first publish and that is normal.
- **No sibling calls.** The context depends on nothing else ([ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads)).

## Migrations

Three embedded migrations live in `internal/adapters/outbound/postgres/migrations/`:

| Migration | Creates |
| --- | --- |
| `0001_node_topology_schema` | `regions`, `nodes`, `node_profile_revisions`, `lanes`, `service_areas` |
| `0002_outbox_and_idempotency` | `outbox_events`, `idempotency_keys` |
| `0003_profile_inbound_limits` | the optional `inbound` JSONB column on `node_profile_revisions` ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)) |

[ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads) names the revision table `node_operational_profiles`; the migration creates `node_profile_revisions` (append-only, enforced by a trigger). The code wins.

All three binaries run them on every boot (idempotent; the advisory lock makes concurrent starts safe), behind a bounded retry: 5 attempts with 1 + 2 + 4 + 8 s backoff (`internal/bootretry`). There is no separate migration job. Point `MIGRATIONS_DATABASE_URL` at the direct Postgres listener, never at PgBouncer.

## Health and readiness

- `GET /healthz`: liveness only. `200 {"status":"ok"}` while the process serves, even mid-drain or with both dependencies down. The mcp and a2a binaries expose the same open `/healthz`.
- `GET /readyz` (api only): `200 {"status":"ready"}` only while the drain gate is open, Postgres answers a ping and a Kafka broker answers a topic-less metadata request. The verdict is cached 3 s and one round is bounded to 2 s. The check names no topics, so probing never creates one and never needs the events topic to exist. While draining it answers `503 {"status":"not_ready"}`; while a dependency is down, `503 application/problem+json` of type `dependency-unavailable` whose `detail` names it (`not ready: kafka`, `not ready: postgres, kafka`).
- **Writes while a dependency is down.** With Kafka down a REST write is still accepted: the aggregate rows and the already-encoded events commit together and wait in `outbox_events`. With Postgres down no write is accepted and `/readyz` fails; in the cluster the 503 removes the pod from the Service endpoints, which is what stops traffic. In the quickstart run, the first two writes were accepted while the broker was down and were published after it returned and the rows were re-armed.

The chart probes: startup on `/healthz` (2 s × 30), liveness on `/healthz` (10 s), readiness on `/readyz` (10 s, 5 s timeout, 3 failures).

## Deployment (Helm)

`charts/node-topology/` renders the api, and optionally mcp, a2a and the console remote (`frontend`, the `node_topology_mfe` Module Federation remote on its own image and no database). Every `config.*` value is one env var (`config.httpAddr`, `config.logLevel`, `config.outboxRelayInterval`, `config.outboxMaxAttempts`, `config.shutdownDrainDelay`); `kafka.brokers` is `KAFKA_BROKERS`; `database.url` creates a Secret, or `database.existingSecret` plus `database.existingSecretKey` and `database.migrationsExistingSecretKey` reference yours. `otel.*` and `environment` render `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME` (`<name>-mcp` and `<name>-a2a` for the other components), `SERVICE_VERSION` and `ENVIRONMENT`. `a2a.agentUrl` is `A2A_AGENT_URL`; empty defaults to the release's in-cluster Service DNS on port 8093.

The chart refuses to render:

- without a database source or without brokers;
- with `terminationGracePeriodSeconds` below 30 or below `config.shutdownDrainDelay` + 20 s (drain delay + HTTP drain 10 s + relay 10 s);
- with a `*_MODE` or `*_CONSUMER_GROUP` in `extraEnv`: the service consumes no topic.

An empty `image.tag` resolves to `v<appVersion>`, the tag the `release` job publishes. The image is `ghcr.io/iqvo/node-topology`. The fleet exposes the api through Kong at `/api/node-topology` (prefix stripped), MCP at `/api/mcp/node-topology` (rewritten to `/mcp`) and A2A at `/api/a2a/node-topology` (rewritten to `/`). `autoscaling.api` is off by default; more than one api replica is safe for the relay (`FOR UPDATE SKIP LOCKED`) but has not been load-tested. Chart tests are in `charts/node-topology/tests/` (`render_test.sh`, `test_service_selectors.py`); packaging status is on [Before the first release](/docs/operations/before-first-release).

## Kafka topics and consumer groups

| Direction | Topic | Notes |
| --- | --- | --- |
| produced | `retailnetwork.node-topology.events` | all ten event types; key and CloudEvents `subject` are the aggregate id; `acks=all`, hash balancer |
| consumed | none | no consumer group exists |

There is no dead-letter topic: nothing is consumed. The producer-side equivalent is the outbox attempt cap, below.

## The outbox relay

Events are written in the same transaction as the aggregates and published later, so a write is accepted while Kafka is down. Rows with `published_at IS NULL` wait and are published in `id` order when the broker is back. Delivery is at-least-once with a stable CloudEvents `id` (minted once, at append); consumers deduplicate on it.

A row whose send failed `OUTBOX_MAX_ATTEMPTS` times (default 20) is excluded from future claims (`AND attempts < $2` in `outbox_drain.go`) and logged once at ERROR with the message `outbox relay: row reached the attempt cap and is excluded from future claims; re-arm it with attempts = 0 after fixing the cause`, with `outbox_id`, `topic`, `event_type`, `key`, `attempts`, `max_attempts` and `error`. Later rows, same key included, overtake it, so this shows up as a gap in consumers, not a stall of the relay. `0` means unlimited.

Inspect and re-arm (verified against a throwaway database, where a broker outage had parked two rows at 20 attempts):

```sql
SELECT id, event_type, attempts, published_at IS NOT NULL AS published, last_error
FROM outbox_events WHERE published_at IS NULL ORDER BY id;

UPDATE outbox_events SET attempts = 0 WHERE id = 1;
```

The relay publishes re-armed rows on its next pass. Ordinary outage rows (below the cap) need no action.

## Sweepers and housekeeping

There is no scheduled sweeper job. Idempotency keys older than 24 h are purged by the next keyed write (`DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'` runs at the start of every keyed request). `outbox_events` rows are never deleted by the service; published rows stay in the table.

## Alerts and dashboards

What exists in `warehouse-infra` on `develop`:

- **Grafana dashboard** uid `warehouse-node-topology` ("Warehouse — Node Topology", `terraform/dashboards/contexts/node-topology.json`): Kong request rate, 5xx ratio and p95 latency for the context's routes; `http_server_request_duration_seconds` by route and process; the business panel `topology_nodes_registered_total` by `outcome`; Go runtime goroutines and memory; Loki live stream, volume by level and errors/warnings (`{app="node-topology"}`).
- **Alert rules** (`terraform/alerting/warehouse-alert-rules.json`): multi-window burn-rate rules on the Kong 5xx ratio and on upstream latency (the 400 ms bucket) for the services matching `httproute.warehouse-systems.node-topology(-mcp|-reports)?`. There is no alert on the outbox backlog, on the attempt cap, or on `/readyz`.

See [Observability](/docs/operations/observability) for the instruments and log fields.

## Failure modes

Short form here; symptom-to-fix tables are on [Troubleshooting](/docs/operations/troubleshooting).

- **Pod crash-looping** (`CrashLoopBackOff`). `kubectl -n warehouse-systems logs deploy/node-topology --previous`. Usual causes are boot errors (each names its variable in the first log line) or migrations failing for the whole retry window (Postgres not up, wrong `MIGRATIONS_DATABASE_URL`). Fix the value or the Secret, then `kubectl -n warehouse-systems rollout restart deploy/node-topology`.
- **Postgres unreachable.** `/readyz` 503 naming `postgres`, REST writes fail, `outbox relay pass failed` at ERROR. Nothing to drain service-side: nothing was accepted, and the relay retries on its own once Postgres answers.
- **Kafka unreachable.** Writes are still accepted and queued; `/readyz` 503 names `kafka` only; `/healthz` stays 200; the relay logs `outbox relay pass failed` each pass and retries every `OUTBOX_RELAY_INTERVAL`; the process never exits over it. Restore the broker and watch the tail drain. Rows that hit the cap need the re-arm above.
- **Downstream blast radius.** The network context map lists eight subscribers to the topic (delivery-promise, last-mile-delivery, linehaul-transportation, node-performance, offer-availability, order-sourcing, vendor-connectivity and, for the inbound limits, seller-inbound); this repository cannot see consumers, so that list is the model's, not something the service checks. A stalled relay shows up as consumer lag and retry logs across the network while their local copies go stale; investigate the relay here first when several of them log at once.

## Routine procedures

- **Restart** (picks up new env or Secret values): `kubectl -n warehouse-systems rollout restart deploy/node-topology`. Add `node-topology-mcp` or `node-topology-a2a` for those components.
- **Logs:** Grafana Loki, `app=node-topology` (JSON `slog` on stdout; `LOG_LEVEL=debug` raises verbosity). Useful messages: `postgres adapters configured`, `outbox relay running`, `outbox relay pass failed`, `reached the attempt cap`, `telemetry configured`.
- **Gateway check:** `GET /api/node-topology/healthz` and `/readyz` through Kong.
- **Graceful shutdown** order: `/readyz` 503, `SHUTDOWN_DRAIN_DELAY`, drain HTTP (10 s), stop and await the relay (10 s), flush telemetry, close the pool.
- **Releases:** the first merge to `main` runs `docker-publish` and `release` (see [Before the first release](/docs/operations/before-first-release) and [Testing](/docs/development/testing)).
