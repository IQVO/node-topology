---
id: integration
title: Integration
sidebar_label: Integration
description: "Every upstream and downstream edge of Node Topology: protocol, contract, failure behaviour, and the edges that do not exist."
---

# Integration

Node Topology is upstream of everything it touches. It **consumes nothing and calls nobody**: there is no Kafka consumer, no consumer group, no inbox table and no outbound HTTP, MCP or A2A client ([ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads); `TestNoOutboundA2AClient` and the dependency fitness tests keep it so). Every relationship below is therefore either a caller that reaches this service, or a subscriber to the topic it publishes.

## Edge map

| Edge | Direction | Protocol | Contract | Pattern |
| --- | --- | --- | --- | --- |
| Seven or eight subscribing contexts | out | Kafka, CloudEvents 1.0 structured mode | `retailnetwork.node-topology.events`, ten event types ([Events](/docs/api-reference/events), `apis/asyncapi.yaml`) | Open Host Service + Published Language; consumers Conformist |
| `vendor-connectivity` | in | REST, in-organisation | `POST /nodes`, `PUT /nodes/{nodeId}/operational-profile` | Customer/Supplier |
| Network-ops planners and consoles | in | REST | the 15 operations of `apis/openapi.yaml` | UI |
| Any context or client | in | REST, MCP, A2A | the read routes, the six MCP tools, the two A2A skills | Open Host Service |
| Console remote | in | REST, read-only `GET` calls from the browser to `${apiOrigin}/api/node-topology` through the gateway (`web/src/config.ts`) | `web/` (`node_topology_mfe`) | UI |

## Upstream

None. The model's `consumes` and `sync_calls` lists for this context are empty, and the code agrees: `cmd/api` opens Postgres and a Kafka producer, `cmd/mcp` and `cmd/a2a` open Postgres only. There is no feature flag, retry policy or fallback to describe because there is nothing to fall back from.

Absent edges, stated so nobody looks for them:

- No fleet service calls this context. The warehouse-systems fleet reaches the network only through the Vendor API of `vendor-connectivity` (its `network-fulfillment`); this context never reads fleet data.
- `customer-ordering` and `shipping-and-tracking` have no edge to it (Separate Ways).
- Nothing publishes on `retailnetwork.node-topology.analytics`: the model names no analytics-only event for this context.

## Downstream: the event stream

The api publishes every event of its four aggregates to one topic, `retailnetwork.node-topology.events`, through the transactional outbox: the aggregate rows and the already-encoded events are written in one Postgres transaction and the always-on relay drains them to Kafka.

| Aspect | Behaviour |
| --- | --- |
| Envelope | CloudEvents 1.0, structured mode, header `content-type: application/cloudevents+json; charset=UTF-8`, `source` `/retailnetwork/node-topology`, `dataschema` `urn:retailnetwork:node-topology:events:<EventName>:v1` |
| Types | `com.retailnetwork.logistics.node-topology.<entity>.<EventName>` with entity `node`, `lane`, `servicearea` or `region` |
| Key and subject | The aggregate id (`nodeId`, `laneId`, `postalPrefix` or `regionId`), so one aggregate's events stay ordered on one partition; `acks=all` |
| Delivery | At least once. The CloudEvents `id` is minted when the event is appended and stored with the outbox row, so a retry republishes the same `id`; consumers deduplicate on it |
| Trace | The active `traceparent` is stored in the row's headers and published as a Kafka header |
| Evolution | A breaking payload change is a new `.v2` type, never a mutation. Consumers dispatch on the full `type` and ignore unknown ones |
| Topic creation | The broker creates the topic on the first write; the service never calls an admin API |

### Subscribers and what they use

The subscriber lists are the network model's (warehouse-docs `network/docs/strategic-design/context-map.md`); this repository cannot see consumers.

| Subscriber | Events it takes | What it builds |
| --- | --- | --- |
| `delivery-promise` | all nine | node profile, region and service-area, and lane views for its promise hot path |
| `order-sourcing` | all nine | node profile and network graph views |
| `linehaul-transportation` | `NodeRegistered`, `NodeOperationalProfileDeclared`, `NodeActivated`, `NodeSuspended`, `NodeRetired`, `LaneOpened`, `LaneClosed` | lanes into departures, cutoffs into pickup departures |
| `offer-availability` | `NodeRegistered`, `NodeActivated`, `NodeSuspended`, `NodeRetired` | whether a listing's node is sellable |
| `vendor-connectivity` | `NodeRegistered`, `NodeActivated`, `NodeSuspended`, `NodeRetired` (and `NodeOperationalProfileDeclared`) | its `VendorNodeView` |
| `last-mile-delivery` | `NodeRegistered`, `ServiceAreaAssigned` | its station view |
| `node-performance` | `NodeRegistered`, `NodeOperationalProfileDeclared` | the declared half it measures against |
| `seller-inbound` | node events, with the inbound limits of the profile | its `NodeInboundView` |

### Failure behaviour

| Failure | What happens | What consumers see |
| --- | --- | --- |
| Kafka down | Writes are still accepted; rows wait in `outbox_events`; `/readyz` answers `503 not ready: kafka` and `/healthz` stays 200; the relay retries every `OUTBOX_RELAY_INTERVAL`; the process does not exit | Nothing, then the backlog in `id` order when the broker returns |
| A row fails `OUTBOX_MAX_ATTEMPTS` times (default 20) | Excluded from future claims and logged once at ERROR; later rows, same key included, overtake it until someone re-arms it with `attempts = 0` | A gap, then the late event with its original `id` |
| Postgres down | No write is accepted; `/readyz` answers 503 naming `postgres` | Nothing new is published |
| Relay goroutine exits | `cmd/api` exits non-zero and Kubernetes restarts it | A pause |
| Crash between the Kafka send and the `published_at` update | The row is sent again with the same `id` | A duplicate to drop |

Procedures are in the [runbook](/docs/operations/runbook#the-outbox-relay) and [Troubleshooting](/docs/operations/troubleshooting).

## Upstream callers: REST

- **`vendor-connectivity`** calls `POST /nodes` and `PUT /nodes/{nodeId}/operational-profile` while processing a vendor submission. It is expected to send its transaction id as the `Idempotency-Key`, so a re-processed submission replays the first answer instead of failing on `vendor-node-code-taken`. This context keeps the invariants (`operator-not-allowed-for-node-type`, `stale-profile-revision`); a 409 or 422 becomes a failed submission on the caller's side. If this service is down the caller gets a connection error or a 503 and decides its own retry.
- **Planners and consoles** drive the other seven commands. A planner registers parcel nodes with operator `NETWORK` here directly.
- **Any context** may use the four reads, but none declares them as a synchronous call: the hot paths of the consumers read their own copies. Reads serve tools, planners and diagnosis.

The fleet gateway (Kong) exposes the api at `/api/node-topology`, MCP at `/api/mcp/node-topology` and A2A at `/api/a2a/node-topology`; details are on the [runbook](/docs/operations/runbook#deployment-helm). REST, MCP and A2A are unauthenticated; reachability is the cluster and gateway boundary.

## Agents: MCP and A2A

`cmd/mcp` and `cmd/a2a` are separate deployables that read the same database through the same read use cases. They are read-only by construction: no write use case is wired, so they cannot append to the outbox, and fitness tests pin that. See [MCP tools](/docs/mcp/tools) for the six tools and the [API overview](/docs/api-reference#a2a) for the A2A agent. A2A is the delegation channel between agents; facts between contexts still travel only as CloudEvents on Kafka.

## Seller-inbound and open gaps

- The `inbound` object of the operational profile ([ADR 0007](/docs/adr/0007-inbound-limits-on-the-operational-profile)) is what `seller-inbound` reads. Since [ADR 0015](/docs/adr/0015-fulfillment-centres-receive-inbound) `FULFILLMENT_CENTER` carries `INBOUND` beside `OUTBOUND` (`internal/domain/node/types.go`), so `GET /nodes/{nodeId}` reports `inbound: true` for a node that receives seller cartons, and `ErrInboundLimitsNotAllowed` (422 `inbound-limits-not-allowed`) refuses new inbound limits on a node type without `INBOUND`. This closes the network model's gap G64.
- A node's suspension or retirement does not close its lanes or reassign its service areas (policy `SuspendedEndpointsUnusable`); consumers treat them as unusable while the node is not `ACTIVE`.
