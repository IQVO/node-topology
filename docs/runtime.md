# Runtime: configuration, probes, shutdown and the outbox

`cmd/api` is the only binary. It runs on real PostgreSQL and real Kafka; there is
no alternative runtime and no log sink.

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `DATABASE_URL` | required | Postgres DSN of the service's pool. A boot error names it when unset. |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | Direct DSN for the boot migrations (golang-migrate holds a session advisory lock, which breaks behind a transaction-pooling proxy). |
| `KAFKA_BROKERS` | required | Comma-separated `host:port` list (relay and `/readyz`). A boot error names it when unset or blank. |
| `HTTP_ADDR` | `:8080` | Listen address. |
| `OUTBOX_RELAY_INTERVAL` | `1s` | Poll interval of the relay (Go duration or seconds). |
| `OUTBOX_MAX_ATTEMPTS` | `20` | Failures before the relay excludes a row; `0` = unlimited (ADR 0005). |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | Wait between flipping `/readyz` to 503 and closing the listener; `0` disables. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

Configuration errors are raised before anything is opened or bound.

## Boot and shutdown

1. Validate the environment. 2. Run the embedded migrations and ping Postgres, both
with exponential backoff (5 attempts, 1 + 2 + 4 + 8 s). 3. Start the outbox relay
(it never dials at construction; a broker that is down only makes passes fail until
it is back). 4. Serve.

On SIGINT/SIGTERM: `/readyz` flips to 503 `not_ready`, wait `SHUTDOWN_DRAIN_DELAY`,
drain HTTP (10 s), stop and await the relay (10 s), close the pool. If the relay
returns on its own the process exits non-zero so the orchestrator restarts it.

## Probes

- `GET /healthz` is liveness: 200 `{"status":"ok"}` while the process serves, even
  while draining or while a dependency is down.
- `GET /readyz` is 200 `{"status":"ready"}` only if the drain gate is open, Postgres
  answers a ping and a Kafka broker answers a metadata request. The request names
  no topics (an empty, non-nil list), so it never creates a topic and does not need
  the output topic to exist; a fresh connection is dialled on every check. While
  draining it answers 503 `{"status":"not_ready"}`; while a dependency is down, 503
  `application/problem+json` of type `dependency-unavailable` naming it. Verdicts
  are cached 3 s.

## The outbox in operation

- Events are written in the same transaction as the aggregates and published later,
  so a write is accepted while Kafka is down; `outbox_events.published_at IS NULL`
  rows wait and are published, in id order, when the broker is back.
- Delivery is at-least-once with a stable CloudEvents `id`; consumers deduplicate.
- A row that failed `OUTBOX_MAX_ATTEMPTS` times is skipped (logged once at ERROR
  with id, topic, type and key) and stays in the table with `attempts` and
  `last_error`. Fix the cause, then re-arm it:
  `UPDATE outbox_events SET attempts = 0 WHERE id = <id>;`. While a row is skipped,
  later events of the same aggregate can overtake it.
- Idempotency keys older than 24 h are purged on the next keyed write.

## Deployment (Helm)

`charts/node-topology/` renders the api alone. Every `config.*` value is one env var from the table above (`config.httpAddr`, `config.logLevel`,
`config.outboxRelayInterval`, `config.outboxMaxAttempts`, `config.shutdownDrainDelay`); `kafka.brokers` is `KAFKA_BROKERS`; `DATABASE_URL` and the optional
`MIGRATIONS_DATABASE_URL` come from a Secret (`database.url` creates one, `database.existingSecret` + `database.existingSecretKey` /
`database.migrationsExistingSecretKey` reference yours; the migrations key is `optional: true`, the binary falls back to `DATABASE_URL`). The chart
refuses to render without a database source or brokers, with `terminationGracePeriodSeconds` below 30 or below `config.shutdownDrainDelay` + 20 s
(drain delay + HTTP drain 10 s + relay 10 s), and with a `*_MODE` or `*_CONSUMER_GROUP` in `extraEnv`: the service consumes no topic. An empty
`image.tag` resolves to `v<appVersion>`, the tag the `release` job publishes. Chart tests: `charts/node-topology/tests/` (run by `chart-tests`; `helm-lint` runs
`ct lint` on pull requests).

## Tests

`make check` (format, vet, build, lint, unit + handler + BDD tests), `make coverage`
(90% over `internal/domain` and `internal/application`), `make mutation` (gremlins
per domain package), `make arch-test` (fitness), `make integration` (testcontainers
Postgres + Kafka: repositories, Drain, the relay, the middleware and the real-wiring
test `cmd/api/e2e_integration_test.go`; needs Docker).
