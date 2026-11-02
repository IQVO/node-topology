# ADR 0012: Adopt the fleet REST API guideline

## Status

Accepted (2026-11-01). Amends [ADR 0006](./0006-http-adapter-idempotency-and-readiness.md)
(the REST adapter) and assumptions A31 and A40 of
[ADR 0002](./0002-aggregates-and-invariants.md).

## Context

The warehouse fleet adopted one REST API guideline (nouns not verbs, every
POST/PATCH idempotent, one answer for key reuse, one replay header, keyset
pagination with one envelope). It is a hard cutover with no aliases, no dual
routes and no toggles, deployed together with the other services of the
cutover set. This service already kept `Idempotency-Replayed` and wrapped every
state-changing route; what remained was the `:verb` routes, the 422 for key
reuse, the per-route key scope, the unpaginated lists and the list envelopes.

## Decision

- **Routes (R1, R2).** The four custom-method routes become transition
  resources; `operationId`s are unchanged, so the MCP tools and A2A skills
  do not move. Status codes and bodies are unchanged.

  | Was | Is |
  | --- | --- |
  | `POST /nodes/{nodeId}:activate` | `POST /nodes/{nodeId}/activations` |
  | `POST /nodes/{nodeId}:suspend` | `POST /nodes/{nodeId}/suspensions` |
  | `POST /nodes/{nodeId}:retire` | `POST /nodes/{nodeId}/retirements` |
  | `POST /lanes/{laneId}:close` | `POST /lanes/{laneId}/closures` |

  `TestRetiredRoutesAreGone` pins that each old route answers 404 or 405.
- **Idempotency (R3-R6, R8).** The key is GLOBAL: `idempotency_keys` is keyed
  by `key` alone (migration 0004 drops the rows of the old `(route, key)` design
  and re-keys the table; the `route` column stays for diagnosis). The request
  fingerprint is `sha256(method, path, body)`. Same key and fingerprint replays
  the first status and body with `Idempotency-Replayed: true`; the same key with
  another method, path or body is **409** `idempotency-key-reused` (was 422).
  A key must be 1 to 128 printable ASCII characters (`idempotency-key-invalid`
  otherwise; it was 200 characters with any non-control character), missing is
  400 `idempotency-key-required`. The key row, the aggregate rows and the outbox
  rows still commit in ONE Postgres transaction (the handler runs in a
  savepoint of the middleware's transaction), a 4xx is stored, a 5xx or a panic
  is not. Every POST was already behind the middleware; no route was unkeyed.
- **PUT routes keep the middleware** (guideline 2.1 exception). `PUT
  /nodes/{nodeId}/operational-profile` APPENDS a profile revision (revisions
  are append-only rows) and `PUT /service-areas/{postalPrefix}` bumps the
  assignment version and publishes `ServiceAreaAssigned`; a keyless retry of
  either would append or publish twice, so a key is required on them.
- **Pagination (R7).** `GET /nodes`, `GET /lanes` and `GET /regions` take
  `limit` (1-500, default 100) and `cursor` and answer `{items, nextCursor}`
  (`nextCursor` absent on the last page). Keyset by the unique id ascending,
  byte-wise (`COLLATE "C"`), in the Postgres repositories and the in-memory
  doubles; the cursor is the fleet `page` helper copied into
  `internal/adapters/inbound/http/page` (base64url JSON of sort key and id; the
  id is its own sort key, so the cursor carries it twice). 400 `invalid-limit`,
  400 `invalid-cursor`, and 400 `unknown-query-parameter` for any parameter the
  collection does not declare (so a client still sending `after`, `offset`,
  `from` or `nextToken` fails loudly). The repositories take a `Window{AfterID,
  Limit}`; `Limit` 0 means every row, which is what the MCP tools and A2A
  skills pass, so they keep returning complete data with their own schemas.
- **Exemptions.** None. `GET /regions` is no longer a bare array.
- **Consumers in this repo.** The console remote `web/src/api.ts` follows
  `nextCursor` (limit 500, bounded at 100 pages) and reads `items`; it sends no
  POST/PATCH. There is no outbound HTTP adapter and no seed script.
- **Enforcement.** `.spectral.yaml` + `.spectral/functions/restGuideline.js`
  (rules R1-R8) run in the existing `api-lint` job with `--fail-severity=warn`.

## Consequences

- Breaking: new URLs, the list envelope, 409 for key reuse, a 128-character key
  limit. It deploys together with the REST cutover set.
- `idempotency_keys` rows of the previous design are discarded at migration
  (they live 24 hours); a client retrying across the deploy gets a fresh
  execution of a request whose first answer was lost.
- The replay lookup is by key alone, so one key cannot be reused across two
  routes any more.
