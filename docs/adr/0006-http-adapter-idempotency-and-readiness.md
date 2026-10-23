# ADR 0006: the HTTP adapter, Idempotency-Key, problem types and readiness

## Status

Accepted (2026-10-23).

## Context

`apis/openapi.yaml` fixes the fifteen operations; the model conventions fix the
four shared problem types; contracts-delta fixes `Idempotency-Key` on every
state-changing route (table `idempotency_keys`, 24 h, replay of the first
response). The fleet requires a dependency-aware `/readyz`, a NUL guard and a
graceful shutdown.

## Decision

1. **Router.** `internal/adapters/inbound/http` is a chi router over the use
   cases (`Server` holds one field per operation; `NewRouter` refuses a server
   without `Idempotency`). Middleware, outermost first: access log, recoverer
   (a panic is a 500 `internal-error` the log still sees), `rejectNUL`, then the
   route. The adapter imports only the application layer and the domain;
   `cmd/api` injects the use cases, the readiness gate and the transaction
   binder (`pgtx.With`), and never imports the memory or repocontract packages.
2. **Operations.** `POST /nodes`, `GET /nodes`, `GET /nodes/{nodeId}`,
   `PUT /nodes/{nodeId}/operational-profile`, `POST /nodes/{nodeId}:activate`,
   `:suspend`, `:retire`, `POST /lanes`, `GET /lanes`, `POST /lanes/{laneId}:close`,
   `PUT /service-areas/{postalPrefix}`, `GET /service-areas/{postalPrefix}`,
   `POST /regions`, `GET /regions`, `GET /regions/{regionId}`; plus the probes
   `GET /healthz` and `GET /readyz` (not part of the business contract). Creates
   answer 201, everything else 200. `GET /nodes` answers `{"nodes":[...]}`,
   `GET /lanes` `{"lanes":[...]}`, `GET /regions` a bare array; an empty result is
   `[]`, never null. The response bodies are the `views` types.
3. **Problems (RFC 9457, `application/problem+json`).** `type` is the slug (the
   OpenAPI `Problem` schema), `title`, `status`, `detail` (the error text; an
   unmapped error is an opaque 500 whose detail is fixed and whose cause is
   logged) and `instance` (the path). Shared: `not-found` 404, `illegal-transition`
   409, `concurrent-update` 409, `validation-failed` 422. One slug per model Err,
   the kebab-case of the name without `Err`: 409 `vendor-node-code-taken`,
   `stale-profile-revision`, `incomplete-operational-profile`, `node-retired`,
   `lane-endpoint-not-active`, `duplicate-lane`, `not-a-delivery-station`,
   `stale-assignment-version`, `region-code-taken`; 422
   `operator-not-allowed-for-node-type`, `subtype-not-allowed`,
   `cutoff-outside-operating-hours`, `sort-dwell-not-allowed`, `lane-loop`,
   `lane-type-pair-not-allowed`, `unknown-region`. Adapter problems: 400
   `malformed-request-body` (bad JSON, unknown field, wrong type, empty body,
   trailing data, NUL), 400 `malformed-request-query` (NUL in the query), 413
   `request-body-too-large` (64 KiB), 404 `not-found` for an unknown route or a
   NUL in the path, 405 `method-not-allowed`, 400 `idempotency-key-required`,
   400 `idempotency-key-invalid` (over 200 characters or a control character),
   422 `idempotency-key-reused`, 503 `dependency-unavailable`.
4. **NUL.** A NUL (U+0000) cannot be stored in Postgres text and would abort the
   transaction. `rejectNUL` answers before the Idempotency middleware records
   anything: 404 for the path, 400 for the query and the body (raw byte or the
   `\u0000` escape; an escaped backslash before `u0000` is not a NUL).
5. **Idempotency-Key.** Required on all eight state-changing routes. The scope is
   `(route, key)` where route is the operation (`POST /nodes/{nodeId}:activate`);
   the request fingerprint is `sha256(method, path, body)`, so the same key on
   another node is another request. The same key and fingerprint replays the first
   status and body (header `Idempotency-Replayed: true`, content type derived from
   the status); the same key with another fingerprint is **422**
   `idempotency-key-reused` (the fleet's choice, not 409). The key row and the
   handler's writes share one transaction: the handler runs in a SAVEPOINT bound
   into its context, a 2xx keeps its writes, a 4xx rolls them back to the
   savepoint but is stored (a repeat answers the same), a 5xx or a panic rolls
   everything back and is never stored. Concurrent requests with one key race on
   `INSERT ... ON CONFLICT DO NOTHING`; the loser blocks until the winner resolves
   and then replays. Keys live 24 h: every keyed write first deletes the expired
   rows (index on `created_at`), so an expired key is a new key and the table does
   not grow without bound. No migration was needed.
6. **Probes.** `/healthz` is liveness: 200 `{"status":"ok"}` while the process
   serves, never flipped. `/readyz` is 200 `{"status":"ready"}` only while the
   drain gate is open AND Postgres answers a ping AND a Kafka broker answers a
   metadata request. The Kafka request names NO topics (an empty, non-nil topic
   list), so it never creates a topic and does not require the output topic to
   exist (the broker creates it on the first write); it dials a fresh connection
   on every check, because kafka-go's cached metadata would keep answering ready
   after the broker is gone. Verdicts are cached 3 s and bounded 2 s, on a context
   detached from the caller. While draining: 503 `{"status":"not_ready"}`; while a
   dependency fails: 503 `dependency-unavailable` naming the dependencies.
7. **Composition root.** `cmd/api` `run(ctx)` validates the environment before
   anything opens (`DATABASE_URL`, `KAFKA_BROKERS`: a boot error names the
   variable; `MIGRATIONS_DATABASE_URL` is an optional direct DSN that falls back
   to `DATABASE_URL`), runs the embedded migrations and pings Postgres with
   backoff (`internal/bootretry`), starts the relay and serves. Shutdown order:
   `/readyz` flips to 503, wait `SHUTDOWN_DRAIN_DELAY` (default 5 s), drain HTTP
   (10 s), stop and await the relay, close the pool. If the relay returns on its
   own the process exits non-zero.

## Consequences

- A client retrying a timed-out write with the same key never duplicates a node,
  lane, region, assignment or event.
- A 4xx is replayed too: after fixing the cause the client sends a new key.
