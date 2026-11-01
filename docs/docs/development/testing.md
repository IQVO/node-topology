---
id: testing
title: Testing
sidebar_label: Testing
description: "The Node Topology test pyramid as it is: unit, godog features, testcontainers integration, fitness tests, mutation, make targets and the CI jobs."
---

# Testing

The pyramid as it is on this branch: a pure domain under a mutation gate, use cases and adapters with unit and `httptest` tests, a godog suite over the real use cases, testcontainers integration tests against real Postgres and Kafka, architecture fitness tests, and spec linting. Counts come from `grep '^func Test'` per package and `grep -c Scenario` per feature file; many tests are table-driven, so the number of cases is larger. They were counted from the source, not from a test run.

| Layer | Count / threshold | Command |
| --- | --- | --- |
| Unit, domain | 85 top-level tests in 5 packages: `node` 53, `servicearea` 11, `lane` 11, `region` 6, `shared` 4 | `make test` |
| Unit, application | 50: `usecases` 39, `views` 11 | `make test` |
| Unit and `httptest`, adapters and `cmd/*` | REST adapter 36, MCP 15, A2A 16, request log 4, Kafka encoder, writer, health and relay sink 18, `kafka/cloudevents` 6, outbox relay 11, Postgres adapter units 27, telemetry 11, memory 4, `cmd/api` 20, `cmd/mcp` 11, `cmd/a2a` 15, `bootretry` 5, ids and clock 2 | `make test` |
| Coverage gate | 90% over `./internal/domain/...,./internal/application/...` (`COVERAGE_PKGS` in the `Makefile`) | `make coverage` |
| BDD (godog / Gherkin) | 9 feature files in `features/`, 104 `Scenario` blocks (see below) | `make bdd` |
| Integration (testcontainers) | 20 files with `//go:build integration` holding 56 top-level functions, including the `TestMain` bootstraps | `make integration` |
| Architecture fitness | 35 top-level tests in 13 files of `internal/architecture` | `make arch-test` |
| Mutation (gremlins) | efficacy 90, mutant coverage 90 (`.gremlins.yaml`), on every package under `internal/domain/` | `make mutation`, `make mutation-full` |
| Spec linting (Spectral) | `apis/openapi.yaml` and `apis/asyncapi.yaml`, `--fail-severity=warn` | CI `api-lint` |
| Chart tests | `charts/node-topology/tests/render_test.sh` and `test_service_selectors.py` | CI `chart-tests` |
| Console remote | `web/`: lint, `tsc -b`, vitest, build | CI `web` |
| Contract (schemathesis) | **none**: no schemathesis configuration and no CI job | none |
| Evals | **none**: `EVALS.md` is the harness template's note and measures nothing in this repository | none |

## Make targets

Every target mirrors a CI job. The `Makefile` is the only definition of the coverage scope and the mutation package list.

| Target | Runs | Notes |
| --- | --- | --- |
| `make help` | prints the target list | default target |
| `make build` | `go build ./...` | compiles the three binaries |
| `make vet` | `go vet ./...` | |
| `make fmt` / `make fmt-check` | `gofmt -w .` / fail if `gofmt -l .` is non-empty | |
| `make lint` | `golangci-lint run --allow-parallel-runners ./...` | pinned `v2.14.0`, config `.golangci.yml` |
| `make test` | `go test ./... -race` | unit, `httptest` and the godog suite (a normal `Test` function); no Docker |
| `make coverage` | `go test ./... -race -coverprofile=coverage.out -coverpkg=./internal/domain/...,./internal/application/...`, then the 90% gate | the CI `test` job runs exactly this |
| `make arch-test` | `go test ./internal/architecture/... -v` | |
| `make bdd` | `go test ./... -run TestFeatures -v` | the godog suite, `features/features_test.go` |
| `make integration` | `go test -tags=integration ./... -race -count=1` | needs only Docker; every test boots its own Postgres and Kafka; not part of `check` |
| `make mutation` | `gremlins unleash <pkg>` for each `./internal/domain/*/` | the fast, blocking gate; the shell glob gates a new domain package the moment it exists |
| `make mutation-full` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30` | the exhaustive scheduled run |
| `make vuln` | `govulncheck ./...` | |
| `make check` | `fmt-check vet build lint test` | the pre-push hook runs this |
| `make check-all` | `check` plus `coverage arch-test bdd` | run before pushing |
| `make check-fast` | `fmt-check vet arch-test` plus `go test` of the packages changed since `HEAD` | the agent Stop hook |
| `make guide-lint` / `make harness-test` | `scripts/harness/guide_lint.py` / `scripts/harness/test_hook.py` | agent-guide checks, not product tests |

Run one package or one test:

```bash
go test ./internal/domain/lane -run TestAllowedPairsPinTheModel -v
go test ./internal/application/usecases -v
```

## Unit tests

- **Domain** (`node`, `lane`, `servicearea`, `region`, `shared`): pure, standard library only. They pin the rows of the model tables (`TestTypeRulesPinTheModel`, `TestAllowedPairsPinTheModel`), the node state machine, the immutable profile revisions, the lane and service-area invariants and the raised events.
- **Use cases** (`internal/application/usecases`): run against the in-memory adapters, which are test doubles only; the shipped binaries never import them (`runtime_no_memory_fitness_test.go` enforces it). `register_metrics_test.go` pins the `topology.nodes.registered` outcomes.
- **REST adapter**: `httptest` over the real router; `routes_test.go`, `middleware_test.go`, `idempotency_test.go`, `errors_test.go` and `telemetry_test.go`.
- **MCP and A2A**: `tools_test.go` pins the tool surface, `governance_test.go` fails the build on a write-verb tool name; `skills_test.go` and `card_test.go` cover the two skills and the Agent Card.
- **Outbox and Kafka**: relay cap and ordering with a fake store (`relay_test.go`), the encoder and CloudEvents helpers.
- **`cmd/*`**: environment parsing and boot configuration, shutdown order and telemetry set-up.

## BDD (godog)

`features/` holds nine `.feature` files and the step definitions beside them; the repository root holds no Go file (`TestNoGoFilesAtRepoRoot`).

| Feature file | Scenario blocks |
| --- | --- |
| `http_api.feature` | 28 |
| `lanes.feature` | 15 |
| `operational_profile.feature` | 15 |
| `node_registration.feature` | 13 |
| `service_areas.feature` | 10 |
| `suspension.feature` | 9 |
| `regions.feature` | 7 |
| `inbound_limits.feature` | 5 |
| `http_idempotency.feature` | 2 |

The runner wires the real use cases over the in-memory adapters, so the suite is fast and needs no Docker. Outline example rows are not counted above.

## Integration (testcontainers)

All files carry `//go:build integration` and boot their own containers through `internal/testing/pgtest` and `internal/testing/kafkatest`. A missing Docker is a failure, never a skip; `TestPostgresIntegrationTestsUseTestcontainers` and `TestKafkaIntegrationTestsUseTestcontainers` fail the build if a test gates on `DATABASE_URL` or hard-codes a broker.

| Suite | Package | What it covers |
| --- | --- | --- |
| Real wiring | `cmd/api` (`TestRealWiring`) | the real entry point on real Postgres and Kafka |
| MCP and A2A against Postgres | `cmd/mcp`, `cmd/a2a` | each reads what the api wrote and never writes |
| Repositories and contract | `outbound/postgres` | the repository contract suite (`internal/adapters/outbound/repocontract`), unit-of-work rollback, concurrency, migrations apply and re-run, schema constraints on `idempotency_keys` and `outbox_events`, trace headers on outbox rows |
| Drain | `outbound/postgres` (`drain_integration_test.go`) | publish in `id` order, batch limit, stop at first failure, the attempt cap, concurrent drains never send a row twice |
| Outbox relay | `outbound/outbox` | the exact CloudEvents published for the outbox rows |
| Idempotency | `inbound/http` | keyed writes over real Postgres |

`make integration` and the CI `integration` job run the same command, `go test -tags=integration ./... -race -count=1`.

## Mutation testing

`.gremlins.yaml` sets `workers: 1`, `timeout-coefficient: 30` and the thresholds `efficacy: 90` and `mutant-coverage: 90`. Gremlins fails when the measured value is less than or equal to the threshold, so a threshold must sit strictly below what the code achieves. Only the domain is mutated. `mutation-fast` loops over each package under `internal/domain/` on every push and pull request; the scheduled `mutation` job runs `./internal/domain` as a whole and the `drift` job runs `scripts/coverage-quality.py` to flag functions with high line coverage and a surviving mutant. A failing scheduled run opens or refreshes a `harness:red` issue. This page records no measured mutation score, because none was run for it.

## Architecture fitness tests

`internal/architecture` holds the dependency-direction rule (`TestHexagonalArchitecture`) and source-scanning sensors. Examples: `TestMCPAdapterDependencyRule`, `TestA2AAdapterDependencyRule`, `TestA2ADepsHoldReadUseCasesOnly` and `TestNoOutboundA2AClient` keep MCP and A2A read-only and inward-looking; `TestNoAuthMiddlewareReintroduced` keeps the surface unauthenticated; `TestEventCatalogueMatchesContract` ties the code's event types to the contract; `TestCloudEventsOnly` and `TestNoEventEnvelopeToggleOrFlatEnvelope` pin the envelope; `TestOutboxRelaysPassAttemptCap`, `TestKafkaWritersSetRequiredAcks` and `TestKafkaWriterTopicNotSetTwice` pin the producer; `TestRuntimeBinaryHasNoInMemoryAdapters` and `TestRuntimeBinaryDoesNotImportTheRepositoryContract` keep test doubles out of `cmd/**`. Sensors that scan source come with a test that proves they can fail (`...SensorBites`, `...SensorFailsOnBadFixtures`).

```bash
make arch-test
```

## CI jobs

`.github/workflows/ci.yml` runs on push and pull request to `main` and `develop`, weekly (Monday 06:00 UTC) and on manual dispatch.

| Job | Runs | When |
| --- | --- | --- |
| `lint` | golangci-lint v2.14.0, `--timeout=5m` | always |
| `guide-lint` | `scripts/harness/guide_lint.py` and `test_hook.py`; advisory (`continue-on-error`) | always |
| `complexity` | golangci-lint with only `gocyclo,gocognit,cyclop,funlen,nestif`, plus an informational gocyclo report | always |
| `test` | `go build`, `go vet`, then `make coverage` (90% gate); uploads `coverage.out` | always |
| `bdd` | `go test ./... -run TestFeatures -v` | always |
| `integration` | `go test -tags=integration ./... -race -count=1` | always |
| `mutation-fast` | gremlins v0.6.0 on each `./internal/domain/*/` | always; blocking |
| `mutation` | `gremlins unleash ./internal/domain --workers 1 --timeout-coefficient 30`; `harness:red` issue on a scheduled failure | schedule and dispatch |
| `drift` | deadcode, `go mod tidy -diff`, knip in `web/`, coverage-quality; advisory | schedule and dispatch |
| `api-lint` | Spectral on both specs, `--fail-severity=warn` | always |
| `vuln` | `govulncheck ./...` | always |
| `helm-lint` | `ct lint --charts charts/node-topology` and `test_service_selectors.py` | pull requests only |
| `chart-tests` | every `charts/node-topology/tests/*.sh` and `test_*.py`; a test that prints `SKIP` fails the job | always |
| `arch-test` | `go test ./internal/architecture/... -v` | always |
| `web` | the console remote: `npm run lint`, `npx tsc -b`, `npm test`, `npm run build` | always |
| `docker-publish` | build and push `ghcr.io/iqvo/node-topology`, cosign signature, SBOM; needs the jobs above | push to `main` |
| `release` | tags the next patch version, retags the image, packages and pushes the Helm chart, creates the GitHub release | push to `main`, after `docker-publish` |

`.github/workflows/docs.yml` builds this site (`npm ci`, `npm run build` in `docs/`) on pull requests and deploys to GitHub Pages only on push to `main`. `.github/workflows/ai-review.yml` is an advisory review on pull requests. Jobs that show as skipped on a pull request (`mutation`, `drift`, `docker-publish`, `release`) are gated by their `if:` conditions; that is expected.

`ci.yml` has no job that regenerates the REST reference and fails on a diff: after any change to `apis/openapi.yaml`, run `npm run clean-api-docs` and `npm run gen-api-docs` in `docs/` yourself.

## Reproducing the docs build

```bash
cd docs
npm ci
npx tsc --noEmit -p .
npm run build
```

The build fails on a broken link or anchor. Mermaid diagrams are checked with `scripts/validate-mermaid.cjs` of the warehouse-docs repository, because `docusaurus build` alone never catches a broken diagram.
