---
id: introduction
title: Introduction
sidebar_label: Introduction
description: "What Node Topology owns and does not own: the retail network's registry of nodes, versioned operational profiles, lanes, regions and service areas."
---

# Node Topology

`node-topology` is the **Supporting** bounded context of the retail network (area Network, CloudEvents tier `logistics`) that owns the network's shape. It is the authoritative registry of the buildings the network runs through (fulfilment centres, inbound cross-docks, returns centres, sortation centres, delivery stations and air hubs), what each building declares it can do (a versioned, immutable operational profile), the directed lanes between active nodes, the regions that partition the network, and which delivery station serves which postal prefix.

It sits at the root of the network's dependency graph. A delivery promise is a claim about a path: this node can have the package ready by this cutoff, a departure leaves on this lane, this station serves this prefix. If two contexts held two copies of "which station serves 01310", the network would promise from one and route from the other. Node Topology is the single owner of those facts, publishes every change as a CloudEvent on `retailnetwork.node-topology.events` through a transactional outbox, and the neighbours keep their own read models of it. It consumes no Kafka topic and calls no sibling context ([ADR 0001](/docs/adr/0001-node-topology-bounded-context), [ADR 0003](/docs/adr/0003-no-consumed-contracts-and-ohs-reads)).

:::info What runs on develop
Three binaries are built from one module and one image: `cmd/api` (REST, migrations, the outbox relay), `cmd/mcp` (a read-only MCP server, [ADR 0008](/docs/adr/0008-mcp-server-adoption)) and `cmd/a2a` (a read-only A2A agent, [ADR 0011](/docs/adr/0011-a2a-inbound-adapter)). A web console remote lives in `web/` ([ADR 0009](/docs/adr/0009-console-remote)). It runs on real Postgres and, for the api, real Kafka only: there is no in-memory runtime. REST, MCP and A2A are unauthenticated; reachability is the cluster and gateway boundary. Packaging is the Helm chart `charts/node-topology`; see [Before the first release](/docs/operations/before-first-release) for what is still open.
:::

## What the context owns

| Aggregate | Where it lives | Rule in one line |
| --- | --- | --- |
| **Node** | `internal/domain/node` | One physical building, of one of six types, run by the `NETWORK` or a `VENDOR`. Lifecycle `PROPOSED` to `ACTIVE`, `SUSPENDED`, `RETIRED` (terminal). Capabilities derive from the type, never from the caller. |
| **OperationalProfile** (inside Node) | `internal/domain/node` | Append-only revisions: operating hours by weekday, holidays, handling time, cutoffs, throughput cap, sort dwell (sortation centres and air hubs only), optional inbound limits. A new declaration never edits an old revision. |
| **Lane** | `internal/domain/lane` | A directed, mode-specific (`GROUND` or `AIR`) connection between two different `ACTIVE` nodes along an allowed type pair; `OPEN` to `CLOSED`, no reopen. |
| **ServiceArea** | `internal/domain/servicearea` | Which `ACTIVE` delivery station serves a postal prefix, versioned by `assignmentVersion`; no unassign. |
| **Region** | `internal/domain/region` | A partition of the network with a unique code; defined once, never renamed or removed. |

## What it does not own

| Not here | Owner |
| --- | --- |
| Scheduled departures on a lane, their capacity and loads (a lane holds only the nominal transit time) | `linehaul-transportation` |
| Waves and routes inside a station's service area | `last-mile-delivery` |
| Measured performance and grades of a node (this context holds the declared half of a node's capability only) | `node-performance` |
| What a node can sell, and how much | `offer-availability` |
| Vendor accounts, ack windows and the Vendor API | `vendor-connectivity` |
| Which node should hold which item before demand arrives | nobody yet (gap G49 in the network model) |
| Suspending a node because it performs badly | nobody automatically: a planner decides |

## Classification and tier

- **Supporting subdomain**, evolution custom-built, business model enabler: every decision depends on the registry, but nobody chooses a network for how it stores its node list. The parcel-node rule and the versioned profile are specific to this network, so it is not Generic ([Subdomain classification](/docs/ddd/subdomain-classification)).
- **Area Network**, CloudEvents tier `logistics`: types look like `com.retailnetwork.logistics.node-topology.<entity>.<EventName>`.

## Main capabilities

1. Register nodes (`POST /nodes`), vendor nodes through `vendor-connectivity` and parcel nodes by a network-ops planner, with the operator, subtype and region rules enforced.
2. Declare operational profile revisions (`PUT /nodes/{nodeId}/operational-profile`), and move nodes through activate, suspend and retire.
3. Open and close lanes, define regions and assign postal prefixes to delivery stations under a strictly increasing `assignmentVersion`.
4. Answer the reads of the network: `GET /nodes`, `GET /nodes/{nodeId}`, `GET /lanes`, `GET /service-areas/{postalPrefix}`, `GET /regions`, over REST, MCP tools and two A2A skills.
5. Publish the nine events of the four aggregates, at least once and in order per aggregate, through the transactional outbox.
6. Make every write retry-safe with the `Idempotency-Key` header (24 h).

Nine commands change state ([Use cases](/docs/ddd/use-cases)); everything else is a read.

## Where to go next

| You want to | Read |
| --- | --- |
| See the layers, binaries, ports and the rules that police them | [Architecture](/docs/overview/architecture), [Runtime](/docs/overview/runtime) |
| Run it and make the first calls | [Quickstart](/docs/overview/quickstart) |
| Know every setting | [Configuration](/docs/operations/configuration) |
| Deploy and operate it | [Runbook](/docs/operations/runbook), [Observability](/docs/operations/observability), [Before the first release](/docs/operations/before-first-release) |
| Fix something that broke | [Troubleshooting](/docs/operations/troubleshooting) |
| Know what tests and CI gates exist | [Testing](/docs/development/testing) |
| See who talks to whom | [Integration](/docs/ecosystem/integration) |
| See the use cases and their invariants | [Use cases](/docs/ddd/use-cases) |
| Read the design as ddd-crew artifacts | [DDD artifacts](/docs/ddd/ddd-artifacts), [Ubiquitous language](/docs/ddd/ubiquitous-language) |
| Read the contracts | [API overview](/docs/api-reference), [Event catalogue](/docs/api-reference/events), [MCP tools](/docs/mcp/tools) |
| Read the decisions | [ADR index](/docs/adr) |

Published at [https://iqvo.github.io/node-topology/](https://iqvo.github.io/node-topology/).
