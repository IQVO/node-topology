---
id: quickstart
title: Quickstart
sidebar_label: Quickstart
description: "Run Node Topology locally against a throwaway Postgres and Kafka, then register a region, a node and a service area."
---

# Quickstart

This walks from a clean checkout to a running `cmd/api` with a real region, node, lane and service area. Every response below was captured from a real run of the binary built from this branch, against a throwaway Postgres and a throwaway Kafka container.

## Prerequisites

- Go (the version in `go.mod`) and `make`.
- Docker, for a local PostgreSQL and one Kafka broker. `make integration` also needs Docker (testcontainers boots its own).
- `curl`. Optional: `golangci-lint` v2.14.0 (`make lint`), `gremlins` v0.6.0 (`make mutation`), `govulncheck` (`make vuln`).

The service has no in-memory mode: `DATABASE_URL` and `KAFKA_BROKERS` are mandatory and a missing one stops the process before it binds a port. In the kind cluster the single shared broker is reachable at `localhost:9092`; the docker-compose Kafka of the old template is retired, so for a laptop-only run start a standalone broker as below.

## Make targets

| Target | What it does |
| --- | --- |
| `make build` / `make vet` / `make fmt` / `make fmt-check` | compile, vet, format in place, fail on unformatted files |
| `make lint` | `golangci-lint run --allow-parallel-runners ./...` |
| `make test` | `go test ./... -race`: unit, handler and godog tests, no Docker |
| `make bdd` | only the godog feature suite (`TestFeatures`) |
| `make coverage` | race tests with `-coverpkg` over `internal/domain` and `internal/application`, gate 90% |
| `make arch-test` | the fitness tests in `internal/architecture/` |
| `make integration` | `go test -tags=integration ./... -race -count=1` (testcontainers Postgres and Kafka) |
| `make mutation` / `make mutation-full` | gremlins per domain package, and the exhaustive run |
| `make vuln` | `govulncheck ./...` |
| `make check` | `fmt-check vet build lint test`, the pre-push gate |
| `make check-all` | `check` plus `coverage arch-test bdd` |

## 1. Start Postgres and Kafka

```bash
docker run -d --name nt-pg -e POSTGRES_USER=nt -e POSTGRES_PASSWORD=nt \
  -e POSTGRES_DB=node_topology -p 55451:5432 postgres:16

docker run -d --name nt-kafka -p 29292:29292 \
  -e KAFKA_NODE_ID=1 -e KAFKA_PROCESS_ROLES=broker,controller \
  -e KAFKA_LISTENERS=PLAINTEXT://:29292,CONTROLLER://:9093 \
  -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:29292 \
  -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT \
  -e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER \
  -e KAFKA_CONTROLLER_QUORUM_VOTERS=1@localhost:9093 \
  -e KAFKA_INTER_BROKER_LISTENER_NAME=PLAINTEXT \
  -e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
  -e KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1 \
  -e KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1 apache/kafka:3.9.0
```

Setting any `KAFKA_*` variable on the `apache/kafka` image replaces its default config, so the broker needs the full set above. The topic `retailnetwork.node-topology.events` is created on the first write (the relay's writer allows auto-creation).

## 2. Run the api

```bash
export DATABASE_URL='postgres://nt:nt@localhost:55451/node_topology?sslmode=disable'
export KAFKA_BROKERS=localhost:29292
export HTTP_ADDR=:18080
go run ./cmd/api
```

Quote the DSN: `?` is a glob character in zsh. On start the binary runs the embedded migrations (seven tables, see [Architecture](/docs/overview/architecture)), pings Postgres, starts the outbox relay and serves. `cmd/mcp` (`MCP_ADDR`, default `:8090`) and `cmd/a2a` (`A2A_ADDR` default `:8093`, plus `A2A_AGENT_URL`) are started the same way with only `DATABASE_URL`; see [Configuration](/docs/operations/configuration).

```bash
curl -s localhost:18080/healthz
curl -s localhost:18080/readyz
```

```json
{"status":"ok"}
{"status":"ready"}
```

If Kafka is not reachable yet, `/readyz` answers `503` with `{"type":"dependency-unavailable", ... "detail":"not ready: kafka"}`; writes still succeed because events wait in the outbox.

## 3. Register a region, a node, a profile

Every state-changing route needs an `Idempotency-Key` header.

```bash
curl -s -X POST localhost:18080/regions -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: qs-region-1' -d '{"code":"SE-BR","name":"Southeast Brazil"}'
```

```json
{"regionId":"region-ceb59c60-0532-4931-bbc4-ca8fd63cb64f","code":"SE-BR","name":"Southeast Brazil"}
```

```bash
curl -s -X POST localhost:18080/nodes -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: qs-node-1' -d '{
    "nodeType":"DELIVERY_STATION","operator":"NETWORK",
    "regionId":"region-ceb59c60-0532-4931-bbc4-ca8fd63cb64f",
    "timezone":"America/Sao_Paulo",
    "location":{"postalCode":"01310-100","countryCode":"BR"}}'
```

```json
{"nodeId":"node-e9740c8e-4adf-43d6-9b21-a335cc45fc76","nodeType":"DELIVERY_STATION","operator":"NETWORK","regionId":"region-ceb59c60-0532-4931-bbc4-ca8fd63cb64f","timezone":"America/Sao_Paulo","location":{"postalCode":"01310-100","countryCode":"BR"},"capabilities":{"outbound":false,"inbound":false,"returns":false,"sort":false,"lastMile":true,"air":false},"status":"PROPOSED"}
```

Capabilities come from the node type, never from the caller. Parcel nodes (`SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB`) must be `NETWORK`-operated; `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK` and `RETURNS_CENTER` must be `VENDOR`-operated and carry a `vendorRef`. Declare revision 1 of the operational profile, then activate:

```bash
curl -s -X PUT localhost:18080/nodes/node-e9740c8e-4adf-43d6-9b21-a335cc45fc76/operational-profile \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: qs-prof-1' -d '{
    "profileRevision":1,"timezone":"America/Sao_Paulo",
    "operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"},"tuesday":{"start":"06:00","end":"22:00"},"wednesday":{"start":"06:00","end":"22:00"},"thursday":{"start":"06:00","end":"22:00"},"friday":{"start":"06:00","end":"22:00"}},
    "handlingTimeMinutes":45,"cutoffs":[{"localTime":"18:00"}],
    "throughputCap":{"value":500,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"}'
```

The response is `200` with the node and its `operationalProfile` (`profileRevision` 1, `holidays` `[]`, `declaredAt` set by the server), still `PROPOSED`. Then:

```bash
curl -s -X POST 'localhost:18080/nodes/node-e9740c8e-4adf-43d6-9b21-a335cc45fc76/activations' \
  -H 'Idempotency-Key: qs-act-1'
```

The node comes back `"status":"ACTIVE"` with `activatedAt`. Repeating the call with the same key replays the first response byte for byte (header `Idempotency-Replayed: true`); a new key on an already ACTIVE node is `409 illegal-transition`.

## 4. Assign a service area and resolve it

```bash
curl -s -X PUT localhost:18080/service-areas/01310 -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: qs-sa-1' \
  -d '{"deliveryStationNodeId":"node-e9740c8e-4adf-43d6-9b21-a335cc45fc76","assignmentVersion":1}'
curl -s localhost:18080/service-areas/01310
```

```json
{"postalPrefix":"01310","deliveryStationNodeId":"node-e9740c8e-4adf-43d6-9b21-a335cc45fc76","regionId":"region-ceb59c60-0532-4931-bbc4-ca8fd63cb64f","assignmentVersion":1}
```

## 5. Open a lane

A lane connects two ACTIVE nodes. Register a vendor fulfilment centre (`operator` `VENDOR`, a `vendorRef`, `subtype` `SORTABLE`), declare a profile with `"declaredBy":"VENDOR"` (a fulfilment centre needs at least one cutoff and a handling time above zero), activate it, then:

```bash
curl -s -X POST localhost:18080/lanes -H 'Content-Type: application/json' -H 'Idempotency-Key: qs-lane-1' \
  -d '{"originNodeId":"node-c35e4af7-0579-4d63-abeb-57506766ba59","destinationNodeId":"node-e9740c8e-4adf-43d6-9b21-a335cc45fc76","mode":"GROUND","nominalTransitMinutes":90}'
```

```json
{"laneId":"lane-02890ecb-486e-4cd8-a833-c67d154b4840","originNodeId":"node-c35e4af7-0579-4d63-abeb-57506766ba59","destinationNodeId":"node-e9740c8e-4adf-43d6-9b21-a335cc45fc76","mode":"GROUND","nominalTransitMinutes":90,"status":"OPEN"}
```

## 6. Errors you will meet first

Errors are RFC 9457 `application/problem+json` and `type` is a slug (full list on the [API overview](/docs/api-reference)). Real responses from the same run:

```text
POST /regions without a key        -> 400 idempotency-key-required
same key, other body               -> 422 idempotency-key-reused
GET /nodes/nope                    -> 404 not-found
POST /regions code SE-BR again     -> 409 region-code-taken
GET /nodes?type=BOGUS              -> 422 validation-failed ("unknown node type")
FULFILLMENT_CENTER as NETWORK      -> 422 operator-not-allowed-for-node-type
profileRevision 1 again            -> 409 stale-profile-revision
same vendorNodeCode twice          -> 409 vendor-node-code-taken
```

A rejected request still consumes its `Idempotency-Key` (4xx outcomes are stored): fix the request and send it with a new key.

## 7. See the events

Each write put a CloudEvent in `outbox_events`; the relay publishes them every second. Read them back from the broker:

```bash
docker exec nt-kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server localhost:29292 \
  --topic retailnetwork.node-topology.events --from-beginning --max-messages 1 \
  --property print.key=true --property print.headers=true
```

```text
content-type:application/cloudevents+json; charset=UTF-8,traceparent:00-0d98...-01	node-e9740c8e-4adf-43d6-9b21-a335cc45fc76	{"specversion":"1.0","id":"06a653e6-79a1-4a15-bbba-ac45b202efeb","source":"/retailnetwork/node-topology","type":"com.retailnetwork.logistics.node-topology.node.NodeOperationalProfileDeclared","subject":"node-e9740c8e-...","datacontenttype":"application/json","dataschema":"urn:retailnetwork:node-topology:events:NodeOperationalProfileDeclared:v1","time":"2026-10-31T23:30:13.649472Z","data":{"nodeId":"node-e9740c8e-...","profileRevision":1,...}}
```

In a clean run the first message is `RegionDefined`. In the captured run the broker was not up when the first two writes happened, so the relay hit `OUTBOX_MAX_ATTEMPTS` on those two rows and skipped them; the profile event overtook them, which is the documented behaviour (see the [runbook](/docs/operations/runbook#the-outbox-relay)). The Kafka key and the CloudEvents `subject` are the aggregate id. The catalogue is on [Events](/docs/api-reference/events).

## 8. Clean up

```bash
docker rm -f nt-pg nt-kafka
```
