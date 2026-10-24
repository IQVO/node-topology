# Node Topology runbook

Node Topology is the retail network's registry bounded context (a domain-driven design study; see the disclaimer in `README.md`): the authoritative source of the network's nodes (fulfillment centres, cross-docks, returns centres, sortation centres, delivery stations, air hubs), their immutable versioned operational profiles, the directed lanes between ACTIVE nodes, the regions, and which delivery station serves which postal prefix. It sits at the root of the network's dependency graph: every change is published as a CloudEvents 1.0 event on `retailnetwork.node-topology.events` through a transactional outbox, it consumes no Kafka topic and calls no sibling context (`docs/adr/0001`, `docs/adr/0003`). Configuration, probes and shutdown semantics are documented developer-side in `docs/runtime.md`; this page is the operator's companion.

## Runtime shape

One binary, one Deployment. `cmd/api` (`cmd/api/main.go`, `cmd/api/run.go`, `cmd/api/relay.go`) is the composition root of everything; the chart (`charts/node-topology`) deploys the single `api` container and there is no other runtime, no log sink and no relay that is off:

- **REST on `:8080`** (`internal/adapters/inbound/http/server.go`): the operations of `apis/openapi.yaml` on a chi router — `POST /nodes`, `PUT /nodes/{nodeId}/operational-profile`, `POST /nodes/{nodeId}:activate|:suspend|:retire`, `POST /lanes`, `POST /lanes/{laneId}:close`, `PUT /service-areas/{postalPrefix}`, `POST /regions` (every state-changing route behind the Idempotency-Key middleware, 24 h replay window, `idempotency_keys` table) and the reads `GET /nodes`, `GET /nodes/{nodeId}`, `GET /lanes`, `GET /service-areas/{postalPrefix}`, `GET /regions`, `GET /regions/{regionId}`. No auth by fleet decision; errors are RFC 9457 problems.
- **Transactional-outbox relay**: the always-on background loop (`internal/adapters/outbound/outbox/relay.go`, started in `cmd/api/relay.go`) that drains unpublished `outbox_events` rows to Kafka every `OUTBOX_RELAY_INTERVAL` (batch 100, one send at a time in `id` order, `FOR UPDATE SKIP LOCKED` claims — `internal/adapters/outbound/postgres/outbox_drain.go`). A full batch is followed immediately by another pass.
- **No consumers.** The service has no Kafka consumer, no consumer group, no inbox table and no `*_CONSUMER_GROUP` variable: it is the root producer of the network's shared local-copy streams.

The relay is a worker: if it returns on its own, the process exits non-zero so Kubernetes restarts it (`cmd/api/run.go`, `serveUntilSignal`; stated in `cmd/api/main.go`).

## Dependencies

- **Postgres** — every write is one transaction (aggregates + outbox rows together). `DATABASE_URL` is the pooled DSN (PgBouncer transaction pooling in the fleet); `MIGRATIONS_DATABASE_URL` is a direct, session-mode DSN used only for the boot migrations, because golang-migrate's session advisory lock cannot survive transaction pooling (`charts/node-topology/values.yaml`, `database.migrationsExistingSecretKey`). Both come from the Secret `node-topology-db` via `secretKeyRef` (the migrations key is `optional`; absent → the binary falls back to `DATABASE_URL`, `cmd/api/config.go`).
- **Kafka** — one cluster, outbound only. The relay publishes to `retailnetwork.node-topology.events` (`internal/adapters/outbound/kafka/encoder.go`, `TopicEvents`; nothing is analytics-only, `apis/asyncapi.yaml`). The topic is auto-created by the first writer: no topic exists until the first publish, and that is normal.
- **No sibling REST calls** — the context depends on nothing else (ADR 0003: `consumes: []`, `sync_calls: []`).

## Health

- `GET /healthz` — liveness only: `200 {"status":"ok"}` while the process serves, even mid-drain or with both dependencies down (`internal/adapters/inbound/http/server.go`; asserted in `middleware_test.go`).
- `GET /readyz` — readiness: `200 {"status":"ready"}` only while the drain gate is open AND Postgres answers a ping AND a Kafka broker answers a topic-less metadata request (verdict cached 3 s, one round bounded 2 s; the check deliberately names no topics, so probing never creates one — `cmd/api/run.go`, `newReadiness`). While draining: `503 {"status":"not_ready"}`; while a dependency is down: `503 application/problem+json` of type `dependency-unavailable` naming it (e.g. `postgres, kafka`).
- **Writes while a dependency is down** (per code, asserted in `cmd/api/e2e_integration_test.go` `assertKafkaDown`): with **Kafka** down a REST write is still accepted — the aggregate rows and the already-encoded events commit together in Postgres and wait in `outbox_events` until the relay can publish. With **Postgres** down no write is accepted (every command is a transaction) and `/readyz` fails; in-cluster the 503 also removes the pod from the Service endpoints, which is what actually stops traffic.

## Configuration

`cmd/api/config.go` reads (and validates; a bad or missing value is a boot error naming the variable, raised before anything is opened or bound):

| Env var | Default | Chart value (`charts/node-topology/values.yaml`) |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | `config.httpAddr` |
| `LOG_LEVEL` | `info` | `config.logLevel` |
| `DATABASE_URL` | required | `database.url`, or `database.existingSecret` + `database.existingSecretKey` (secretKeyRef) |
| `MIGRATIONS_DATABASE_URL` | = `DATABASE_URL` | `database.migrationsExistingSecretKey` (optional secretKeyRef) |
| `KAFKA_BROKERS` | required | `kafka.brokers` |
| `OUTBOX_RELAY_INTERVAL` | `1s` | `config.outboxRelayInterval` (empty = not rendered) |
| `OUTBOX_MAX_ATTEMPTS` | `20` (`0` = unlimited) | `config.outboxMaxAttempts` |
| `SHUTDOWN_DRAIN_DELAY` | `5s` (`0` disables) | `config.shutdownDrainDelay` (empty = not rendered) |

Boot runs the embedded migrations and pings Postgres behind a bounded retry (5 attempts, 1+2+4+8 s backoff, `internal/bootretry`); the startup probe on `/healthz` (2 s × 30) covers that window. The chart refuses to render without a database source or brokers, refuses any `*_MODE` / `*_CONSUMER_GROUP` value, and requires `terminationGracePeriodSeconds` (30) above drain delay + 20 s — the binary's worst-case shutdown is drain delay + HTTP drain (10 s) + relay stop (10 s).

## Alerts and dashboards

- **Grafana dashboard** uid `warehouse-node-topology` (warehouse-infra). Pod-level panels (restarts, CPU/memory from the kubelet) work today.
- **Alerts**: pod-restart alerts exist in warehouse-infra's alert rules. There is no service-specific alert yet.
- **Honest gap**: the service instruments no business metrics of its own (no Prometheus registry for domain counters/histograms in `cmd/api`), so the dashboard's business-metric panels stay empty until the service instruments itself. Until then the two signals that exist are pod health and log-derived behaviour (Loki, below).

## Failure modes

- **Pod crash-looping** (`CrashLoopBackOff`). *Diagnose*: `kubectl -n warehouse-systems logs deploy/node-topology --previous`. Common causes are boot errors — every missing/invalid env var is named in the first log line (`cmd/api/config.go`) — or migrations failing for the full retry window (Postgres not up, wrong `MIGRATIONS_DATABASE_URL`). *Action*: fix the value or the Secret, then `kubectl -n warehouse-systems rollout restart deploy/node-topology`.
- **Postgres unreachable.** *Symptom*: `/readyz` 503 `dependency-unavailable` naming `postgres`; REST writes fail; `outbox relay pass failed` at ERROR (claims need Postgres too). *Diagnose*: check PgBouncer and the Postgres behind it. *Action*: nothing service-side to drain — the relay retries on its own interval once Postgres answers; no data is lost (the outbox lives in Postgres, so while it is unreachable nothing was accepted to lose).
- **Kafka unreachable.** *Verified behaviour*: REST writes are still accepted and queued — rows accumulate in `outbox_events` with `published_at IS NULL` and publish in `id` order when the broker returns; `/readyz` 503 names `kafka` only, `/healthz` stays 200; the relay logs `outbox relay pass failed` every pass and retries every `OUTBOX_RELAY_INTERVAL`; the process never exits over it (only a worker *returning* kills the pod). *Action*: restore the broker; watch the outbox tail drain (SQL below). No re-arm is needed for ordinary outage rows — only rows that hit the attempt cap need it.
- **Outbox relay stuck / a row parked at max attempts.** The table is `outbox_events` (`internal/adapters/outbound/postgres/migrations/0002_outbox_and_idempotency.up.sql`: `published_at TIMESTAMPTZ`, `attempts INTEGER NOT NULL DEFAULT 0`, `last_error TEXT`). A row whose send failed `OUTBOX_MAX_ATTEMPTS` (20) times is excluded from future claims (`AND attempts < $2` in `outbox_drain.go`) and logged ONCE at ERROR — `outbox relay: row reached the attempt cap and is excluded from future claims; re-arm it with attempts = 0 after fixing the cause` — with `outbox_id`, `topic`, `event_type`, `key`, `attempts`, `error`. Later rows (same key included) overtake it, so this shows up as a gap in consumers, not a stall of the relay. *Diagnose*: `SELECT id, topic, event_type, key, attempts, last_error, created_at FROM outbox_events WHERE published_at IS NULL ORDER BY id;` and read `last_error`. *Action*: fix the cause, then re-arm exactly as the log says: `UPDATE outbox_events SET attempts = 0 WHERE id = <id>;`. A relay that is genuinely stuck (no pass logs at all) means the worker died, which exits the pod — the pod-restart alert catches it.
- **Downstream blast radius.** Seven sibling contexts subscribe to `retailnetwork.node-topology.events` with live consumer wiring (verified in their repos: `internal/adapters/inbound/kafka`): delivery-promise, last-mile-delivery, linehaul-transportation, node-performance, offer-availability, order-sourcing and vendor-connectivity. A stalled relay here does not fail their reads immediately — it surfaces as consumer lag and retry logs across the network while their local copies go stale, so investigate node-topology's relay first when several of those log at once.

## Operational actions

- Restart (picks up new env/Secret values): `kubectl -n warehouse-systems rollout restart deploy/node-topology`.
- Logs: Grafana Loki, `app=node-topology` (the pod writes JSON slog to stdout; `LOG_LEVEL` raises verbosity to `debug`). Useful patterns: `postgres adapters configured`, `outbox relay running`, `outbox relay pass failed`, `reached the attempt cap`.
- REST check through the fleet gateway: Kong route `/api/node-topology` (prefix stripped), e.g. `GET /api/node-topology/healthz` and `GET /api/node-topology/readyz`.
- Do not point `MIGRATIONS_DATABASE_URL` at the pooled listener: migrations need the direct DSN (advisory lock). The migrations are embedded in the image and run on every boot; there is no separate migration job.
