---
id: troubleshooting
title: Troubleshooting
sidebar_label: Troubleshooting
description: "Node Topology symptoms mapped to cause, check and fix: boot errors, readiness, the outbox relay, idempotency and the REST problem types."
---

# Troubleshooting

Symptom, cause, check, fix, built from the failure modes visible in the code. Log messages and problem slugs are the literal strings in `cmd/`, `internal/adapters/inbound/http/problems.go` and `internal/adapters/outbound/outbox/relay.go`. The table names are in the [runbook](/docs/operations/runbook#migrations). This service has no outbound HTTP, MCP or A2A client and consumes no Kafka topic, so there is no circuit breaker and no consumer lag to chase; the dependencies that can fail are Postgres and, for the relay and `/readyz`, Kafka.

## Boot

| Symptom | Likely cause | Check | Fix |
| --- | --- | --- | --- |
| `cmd/api` exits 1 at once: `missing required environment variable: DATABASE_URL` | `DATABASE_URL` unset or empty. There is no in-memory runtime to fall back to | The ERROR line `service exited with error` or the pod's `kubectl logs` | Set it ([Configuration](/docs/operations/configuration)) |
| `cmd/api` exits 1: `missing required environment variable: KAFKA_BROKERS (comma-separated host:port list)` | `KAFKA_BROKERS` unset, or only blanks and commas | Pod env | Set a `host:port` list. The api needs Kafka for the relay and `/readyz`; `cmd/mcp` and `cmd/a2a` do not |
| `cmd/mcp` or `cmd/a2a` exits 1: `DATABASE_URL is required: this context runs on real Postgres` | Empty `DATABASE_URL` | Pod env | Set it |
| `cmd/a2a` exits 1: `A2A_AGENT_URL is required: the absolute http(s) URL clients dial, advertised on the Agent Card` or `A2A_AGENT_URL must be an absolute http(s) URL with a host, got "..."` | The Agent Card must advertise a dialable URL, so there is no default; checked before anything else starts | Pod env | Give an absolute `http(s)` URL with a host. In the chart leave `a2a.agentUrl` empty to get the in-cluster Service DNS on port 8093 |
| Exits 1 after about 15 s: `run migrations (after 5 attempts): ...` or `ping postgres (after 5 attempts): ...` | Postgres unreachable, wrong DSN, or a migration that fails. The retry is 5 attempts with 1, 2, 4 and 8 s waits (`internal/bootretry`) | WARN lines `retrying` carry `op`, `attempt` and `err`; try `psql` from the same network | Fix the DSN or network. Fail-closed on purpose: the service never boots without its database |
| Migrations hang, or fail on the advisory lock | `DATABASE_URL` goes through a transaction-pooling PgBouncer and `MIGRATIONS_DATABASE_URL` is not set. golang-migrate takes a session advisory lock | Is the DSN a pooler address? | Set `MIGRATIONS_DATABASE_URL` to a direct Postgres DSN |
| Every boot fails the same way after one crashed migration | `schema_migrations.dirty = true`: golang-migrate refuses to continue | `SELECT version, dirty FROM schema_migrations` | Repair the schema by hand, then force the version with golang-migrate. Back up first |
| A warning `invalid OUTBOX_RELAY_INTERVAL; using the default`, `invalid OUTBOX_MAX_ATTEMPTS; using the default` or `ignoring invalid SHUTDOWN_DRAIN_DELAY` | Malformed or out-of-range value. The service starts with the default instead of failing | The WARN line carries `value` and `default` | Fix the value. A typo never removes the attempt cap: it falls back to 20 |
| `cmd/api` exits non-zero later with `outbox-relay stopped unexpectedly: exiting so the orchestrator restarts the service` | The relay goroutine returned by itself | Logs just before the exit | Kubernetes restarts the pod. Persistent exits point at the cause logged before it |
| Telemetry shows nothing in Jaeger or Prometheus but the service works | The Collector is unreachable. Export never blocks, so the only symptom is missing data | `LOG_LEVEL=debug` shows `opentelemetry error` lines; check `OTEL_EXPORTER_OTLP_ENDPOINT` in the `telemetry configured` boot line | Point the variable at the Collector |

## Readiness and probes

| Symptom | Likely cause | Check | Fix |
| --- | --- | --- | --- |
| `GET /readyz` is `503` with body `{"status":"not_ready"}` | The pod is shutting down: the drain gate flips on SIGINT or SIGTERM | Is the pod terminating? | Nothing to fix; a new pod takes over |
| `GET /readyz` is `503 application/problem+json`, type `dependency-unavailable`, detail `not ready: kafka` (or `postgres`, or `postgres, kafka`) | No broker answered a topic-less metadata request, or the Postgres ping failed. The verdict is cached 3 s and a round is bounded to 2 s | WARN `readiness: dependency is not ready` has `dependency` and `error` | Restore the broker or database; the process stays up and recovers by itself. A missing topic never causes this: the check names no topic |
| `/healthz` is 200 but requests fail with 500 | A single query failing for another reason (statement timeout, constraint, bug); `/readyz` only proves Postgres answers a ping | ERROR `request failed` carries the real error (the response says only `an unexpected error occurred`) | Fix the cause shown in the log |
| Statements fail with `canceling statement due to statement timeout` | The pool sets `statement_timeout = 5s` on every connection and a pool of at most 10 connections per process (`postgres/pool.go`) | `request failed` lines; slow queries in Postgres | Investigate the query. Both limits are compiled in |
| `cmd/mcp` or `cmd/a2a` answers `/healthz` but a tool or skill fails | These binaries have no `/readyz`; they only need Postgres | `mcp tool failed with an unexpected error` or `a2a skill failed` in the logs | Fix Postgres connectivity |

## The outbox and Kafka

| Symptom | Likely cause | Check | Fix |
| --- | --- | --- | --- |
| Writes succeed but no event reaches the topic; `outbox_events.published_at` stays NULL | The broker is unreachable. Writes are accepted and queued by design | ERROR `outbox relay pass failed` each pass with the cause; `/readyz` names `kafka`; `SELECT id, event_type, attempts, last_error FROM outbox_events WHERE published_at IS NULL ORDER BY id` | Restore the broker. Rows drain in `id` order |
| ERROR `outbox relay: row reached the attempt cap and is excluded from future claims; re-arm it with attempts = 0 after fixing the cause` | A poison row, or an outage longer than `OUTBOX_MAX_ATTEMPTS` passes (default 20, one pass per `OUTBOX_RELAY_INTERVAL`, default 1 s). Later rows, the same key included, overtake the capped one | `SELECT id, event_type, attempts, last_error FROM outbox_events WHERE published_at IS NULL AND attempts >= 20` | Fix the cause, then `UPDATE outbox_events SET attempts = 0 WHERE id = <id>`. Consumers see the event late, out of order, with its original CloudEvents `id`. `OUTBOX_MAX_ATTEMPTS=0` removes the cap and lets a poison row block |
| Downstream contexts report a stale node, lane or service area | The relay is stalled or rows hit the cap | `outbox relay running` at boot; backlog query above | Restore the relay; re-arm capped rows |
| A consumer sees the same event twice | At-least-once delivery after a crash between the send and the `published_at` update. The CloudEvents `id` is stable | Compare the `id` | Consumers deduplicate on `id`. Nothing to fix here |
| Topic `retailnetwork.node-topology.events` does not exist yet | Nothing was published. The first write creates it (the relay's writer allows auto-creation) | `kafka-topics.sh --list` | Normal on a fresh broker. On a broker with auto-creation disabled, create the topic |
| Pod restart loses nothing | Expected: events are committed with the aggregate | `outbox_events` rows survive | none |

## Idempotency

| Symptom | Likely cause | Check | Fix |
| --- | --- | --- | --- |
| `400 idempotency-key-required` | A `POST`, `PUT` or `:action` call without `Idempotency-Key`. Every state-changing route needs one; there is no unkeyed mode | The request headers | Send a unique key per logical request |
| `400 idempotency-key-invalid` | Key longer than 200 characters or containing a control character | The key | Use up to 200 printable characters |
| `422 idempotency-key-reused` | The same key (per route) with a different method, path or body; or the key expired while the request was in flight | Compare the two requests | Use a new key, or resend the original request to get its stored answer |
| A rejected request keeps answering the same 4xx | 2xx and 4xx outcomes are stored for 24 h and replayed byte for byte with header `Idempotency-Replayed: true`. A 5xx is never stored | The response header | Fix the request and send it with a **new** key |
| A retry after a 500 does the work again | Expected: a 5xx rolls everything back and is not stored, so the same key can retry | none | none |

## REST problem types

Every error is `application/problem+json` with `type` (the slug), `title`, `status`, `detail` and `instance` (the request path). The catalogue is `problemCatalogue` in `internal/adapters/inbound/http/problems.go`.

| Status | Slug | When | What to do |
| --- | --- | --- | --- |
| 400 | `malformed-request-body` | Not valid JSON for the operation: unknown field, wrong type, trailing data, empty body, or a NUL character | Fix the body against `apis/openapi.yaml` |
| 400 | `malformed-request-query` | A NUL character in the query string | Remove it |
| 413 | `request-body-too-large` | Body over 64 KiB | Send less |
| 400 | `idempotency-key-required`, `idempotency-key-invalid` | See Idempotency above | |
| 404 | `not-found` | Unknown id, unassigned prefix, or no route matches | Check the id; `GET /nodes/{nodeId}` before acting |
| 405 | `method-not-allowed` | Wrong verb on a known path | Check the method |
| 409 | `illegal-transition` | A lifecycle move the state machine refuses, for example activating an ACTIVE node, suspending a PROPOSED one, or closing a CLOSED lane | `GET` the aggregate and read its `status` |
| 409 | `concurrent-update` | Two writers raced on one aggregate and the version guard of the loser failed | Re-read and retry |
| 409 | `vendor-node-code-taken` | The vendor already registered a node with this `vendorNodeCode` | Use the existing node |
| 409 | `stale-profile-revision` | `profileRevision` is not above the node's current one | `GET` the node and send current + 1 |
| 409 | `incomplete-operational-profile` | Activating a FULFILLMENT_CENTER whose current profile has no cutoff or a handling time of zero, or declaring such a revision on an ACTIVE fulfilment centre | Declare a complete revision, then activate |
| 409 | `node-retired` | Any transition or profile declaration on a RETIRED node, which is terminal | Register a new node |
| 409 | `lane-endpoint-not-active` | Either end of the lane is not an ACTIVE node | Activate the node first |
| 409 | `duplicate-lane` | An OPEN lane with the same origin, destination and mode exists | Reuse it |
| 409 | `not-a-delivery-station` | The target of a service-area assignment is not an ACTIVE delivery station | Activate the station or pick another |
| 409 | `stale-assignment-version` | `assignmentVersion` is not above the current one for the prefix | Resolve the prefix and send current + 1 |
| 409 | `region-code-taken` | A region with this `code` exists | Use the existing region |
| 422 | `idempotency-key-reused` | See Idempotency above | |
| 422 | `operator-not-allowed-for-node-type` | Parcel nodes (sortation centre, delivery station, air hub) must be `NETWORK`-operated; fulfilment centre, inbound cross-dock and returns centre must be `VENDOR`-operated | Fix `operator` |
| 422 | `subtype-not-allowed` | A `subtype` on anything but a fulfilment centre | Drop it |
| 422 | `cutoff-outside-operating-hours` | A cutoff falls outside the operating hours of its day | Fix the profile |
| 422 | `sort-dwell-not-allowed` | `sortDwellMinutes` on a node that does not sort | Drop it |
| 422 | `lane-loop` | Origin and destination are the same node | Pick two nodes |
| 422 | `lane-type-pair-not-allowed` | This pair of node types may not be connected | See the allowed pairs in [use cases](/docs/ddd/use-cases) |
| 422 | `unknown-region` | `regionId` does not exist | Define the region first |
| 422 | `validation-failed` | Any value-object rule: bad enumeration value, malformed timezone or postal code, empty required field | The `detail` text names the field |
| 500 | `internal-error` | Unmapped error or handler panic; `detail` is always `an unexpected error occurred` | Read the `request failed` or `panic in handler` log line |
| 503 | `dependency-unavailable` | `GET /readyz` only | See Readiness above |

MCP tools return the same vocabulary as text: `not-found: ...`, `validation-failed: ...` or `internal-error: an unexpected internal error occurred` in an `isError` tool result. A2A skills answer the same slugs as problem objects.
