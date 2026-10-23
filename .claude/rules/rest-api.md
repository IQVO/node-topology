---
paths:
  - "internal/adapters/inbound/http/**"
  - "apis/openapi*.yaml"
  - "apis/openapi/**"
---

# REST API (inbound adapter)

Route -> use case, kept in sync with `apis/openapi.yaml` (the routes are
registered in `internal/adapters/inbound/http/handlers_nodes.go` and
`handlers_other.go`, wired in `cmd/api/run.go` `buildServer`):

- `POST /nodes` -> `RegisterNode` (201)
- `GET /nodes?type=&region=&status=` -> `ListNodes` (`{"nodes":[...]}`)
- `GET /nodes/{nodeId}` -> `GetNode`
- `PUT /nodes/{nodeId}/operational-profile` -> `DeclareOperationalProfile`
- `POST /nodes/{nodeId}:activate` -> `ActivateNode`
- `POST /nodes/{nodeId}:suspend` -> `SuspendNode` (body `{"reason"}`)
- `POST /nodes/{nodeId}:retire` -> `RetireNode`
- `POST /lanes` -> `OpenLane` (201)
- `GET /lanes?origin=&destination=` -> `ListLanes` (`{"lanes":[...]}`)
- `POST /lanes/{laneId}:close` -> `CloseLane`
- `PUT /service-areas/{postalPrefix}` -> `AssignServiceArea`
- `GET /service-areas/{postalPrefix}` -> `ResolveServiceArea`
- `POST /regions` -> `DefineRegion` (201)
- `GET /regions` -> `ListRegions` (a bare array)
- `GET /regions/{regionId}` -> `GetRegion`
- `GET /healthz` (liveness), `GET /readyz` (readiness): not business operations.

## Conventions

- **Errors** are RFC 9457 `application/problem+json`: `type` is a kebab-case
  slug (`internal/adapters/inbound/http/problems.go`), plus `title`, `status`,
  `detail`, `instance`. Shared: `not-found` 404, `illegal-transition` 409,
  `concurrent-update` 409, `validation-failed` 422; one slug per model `Err...`
  (`vendor-node-code-taken`, `lane-loop`, `stale-assignment-version`, ...); an
  unmapped error is an opaque 500 `internal-error`. A new `Err` needs a catalogue
  row and a row in `errors_test.go`.
- **Idempotency-Key** is required on all eight state-changing routes
  (`idempotency.go`): same key + same request replays the first response, same
  key + other request is 422 `idempotency-key-reused`, missing key 400
  `idempotency-key-required`, keys live 24 h in `idempotency_keys`. A new
  state-changing route registers through `Server.keyed`, never `r.Post`.
- **NUL** (U+0000) is rejected before anything else (`nul.go`): 404 in the path,
  400 `malformed-request-query` / `malformed-request-body` elsewhere.
- Bodies are decoded strictly (`decodeJSON`): unknown fields are 400
  `malformed-request-body`; a value the domain refuses is 422.
- **Handlers hold no business logic**: decode, call a use case, encode the view.
- **Auth**: none. REST is unauthenticated, as in the rest of the network
  (`internal/architecture/fitness_test.go` `TestNoAuthMiddlewareReintroduced`
  keeps it so).
- Unit tests (`*_test.go` in the adapter) run the real use cases over the
  memory test doubles with a key-presence stub; the real middleware is proven on
  Postgres in `idempotency_integration_test.go`.
