# node-topology

harness-template: v4

Retail network, bounded context Node Topology (Supporting, logistics tier). Owns Node, Lane, ServiceArea and Region and publishes them; consumes no Kafka topic and calls no sibling.
Design source of truth: IQVO/warehouse-docs `network/model/model.yaml` (context node-topology). This service belongs to the retail network, not the warehouse-systems fleet.
This project is a domain-driven design study, not associated with any company: never name a real company, brand or product in code, comments, ADRs, specs, tests or fixtures (README `## Disclaimer`).

## Commands
`make check` (fmt, vet, build, lint, unit + handler + BDD; the pre-push gate), `make coverage` (>= 90% over internal/domain + internal/application), `make mutation` (gremlins per internal/domain/<pkg>, thresholds below measurement),
`make arch-test`, `make integration` (testcontainers Postgres + Kafka, needs Docker; build tag `integration`), `make vuln`. Lint: `golangci-lint run --allow-parallel-runners ./...`.
Guides: `python3 scripts/harness/repo_lint.py`, `python3 scripts/harness/guide_lint.py` must stay clean. Never gate a commit on a piped command.

## Layout (hexagonal)
`internal/domain/{shared,node,lane,servicearea,region}` pure Go; `internal/application/{ports (interfaces only),repository (filters),views,usecases,outbox}`;
`internal/adapters/inbound/http` (chi); `internal/adapters/outbound/{postgres,kafka,outbox,clock,ids,memory,repocontract}`; `internal/adapters/kafka/cloudevents` (the ONLY envelope builder);
`internal/testing/{pgtest,kafkatest}` (testcontainers helpers); `cmd/api` (composition root, `run(ctx)`); `features/` (godog, never a .go file at the repo root). Migrations are embedded in `internal/adapters/outbound/postgres/migrations`.

## Runtime rule
Real PostgreSQL and Kafka only. `DATABASE_URL` and `KAFKA_BROKERS` are mandatory (boot error naming the variable, raised before the listener binds; `MIGRATIONS_DATABASE_URL` is an optional direct DSN, falls back to `DATABASE_URL`).
The outbox relay is always on (`OUTBOX_MAX_ATTEMPTS` default 20, 0 = unlimited; `OUTBOX_RELAY_INTERVAL` 1s) and the process exits non-zero when it ends on its own.
`/healthz` = liveness; `/readyz` = drain gate + Postgres ping + Kafka metadata request with an EMPTY non-nil topic list (fresh connection per check; it never needs the topic to exist).
Shutdown: /readyz 503, `SHUTDOWN_DRAIN_DELAY` (5s), drain HTTP, stop relay, close pool. cmd/** never imports internal/adapters/outbound/memory or repocontract. No authentication on REST. See docs/runtime.md.

## REST
chi router implementing every operation of apis/openapi.yaml (15 routes) plus /healthz, /readyz. Every state-changing route takes `Idempotency-Key` (table `idempotency_keys`, key = (route, key), 24h, replays the first response, same key + other request = 422 `idempotency-key-reused`).
NUL: 404 in the path, 400 `malformed-request-query|body` elsewhere. Errors are RFC 9457 problem+json, `type` = slug: not-found, illegal-transition, concurrent-update, validation-failed + one kebab-case slug per model Err (ADR 0006).

## Events: CloudEvents 1.0 is MANDATORY
type `com.retailnetwork.logistics.node-topology.<entity>.<Event>` (entity: node, lane, servicearea, region); source `/retailnetwork/node-topology`; dataschema `urn:retailnetwork:node-topology:events:<Event>:v1`;
all nine events on `retailnetwork.node-topology.events` (nothing analytics-only); subject and Kafka key = aggregate id; header `content-type: application/cloudevents+json; charset=UTF-8`.
Use internal/adapters/kafka/cloudevents only. Transactional outbox: aggregates + outbox rows in ONE Postgres transaction, id minted once at append, at-least-once delivery. No `warehouse.*` topic, ever.
A new event: domain struct + apis/asyncapi.yaml + ADR 0004 `type:` line in the SAME commit + encoder payload + golden test (.claude/skills/how-to-add-an-integration-event).

## Domain
Aggregates, invariants and assumptions A1-A42 are in docs/adr/0002; outbox/relay/cap in 0005; HTTP, idempotency, readiness in 0006. Parcel nodes are network-operated only. Profile revisions are append-only rows, never edited.

## Workflow
GitFlow: feature/* -> PR into develop, CI green before merge, never push develop or main. lefthook: pre-commit fmt/vet/lint, pre-push `make check`. Commit and push after each step.
Helm chart: charts/node-topology (api only, no consumer group; tests in charts/node-topology/tests, rows 5-9 of docs/before-first-release.md). Open follow-up: repository secrets (row 11).

A2A: `cmd/a2a` (port 8093) is this context's read-only Agent2Agent agent, skills `serving_station` (read), `find_nodes` (read), decided in `docs/adr/0011-a2a-inbound-adapter.md`; delegation only (facts stay CloudEvents on Kafka), unauthenticated like REST and MCP; rules in `.claude/rules/fleet/a2a.md`.
