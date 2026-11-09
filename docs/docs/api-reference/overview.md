---
id: overview
title: API overview
sidebar_label: API overview
slug: /api-reference
description: "Node Topology's interfaces (REST, events, MCP, A2A), its endpoint table and the contract conventions: idempotency, versioning, problem types and the absence of auth."
---

# API overview

:::info Implemented by cmd/api, cmd/mcp and cmd/a2a
`cmd/api` serves every REST route below (`internal/adapters/inbound/http`) on `HTTP_ADDR` (default `:8080`). The contracts are `apis/openapi.yaml` and `apis/asyncapi.yaml`, pinned by [ADR 0001](/docs/adr/0001-node-topology-bounded-context), [ADR 0002](/docs/adr/0002-aggregates-and-invariants) and [ADR 0006](/docs/adr/0006-http-adapter-idempotency-and-readiness). The router registers the 15 operations of the spec plus `/healthz` and `/readyz`, which the spec does not list. See the [Quickstart](/docs/overview/quickstart) for real responses.
:::

| Interface | Source of truth | Notes |
| --- | --- | --- |
| REST | `apis/openapi.yaml` (OpenAPI 3.0.3, version 0.1.0) | The pages under *REST* are generated from the spec with `npm run gen-api-docs`; do not edit them by hand. The only server the spec names is `http://localhost:8080`; through Kong in the fleet the api is at `/api/node-topology` with the prefix stripped. |
| Events | `apis/asyncapi.yaml` (AsyncAPI 3.0.0, version 1.0.0) | Nine CloudEvents on `retailnetwork.node-topology.events`; summarised on the [event catalogue](/docs/api-reference/events), every field on [Domain events](/docs/ddd/domain-events). |
| MCP | [ADR 0008](/docs/adr/0008-mcp-server-adoption), `internal/adapters/inbound/mcp` | `cmd/mcp` serves Streamable HTTP on `/mcp` and `/` (port 8090; through Kong `/api/mcp/node-topology`). Six read-only tools, no write tool; see [MCP tools](/docs/mcp/tools). |
| A2A | [ADR 0011](/docs/adr/0011-a2a-inbound-adapter), `internal/adapters/inbound/a2a` | `cmd/a2a` serves JSON-RPC 2.0 on `/` and `/a2a` (port 8093; through Kong `/api/a2a/node-topology`). Two read-only skills; see below. |

## Endpoints

All routes are on `cmd/api`. State-changing routes need an `Idempotency-Key`.

| Tag | Endpoint | Operation id | Success |
| --- | --- | --- | --- |
| Nodes | `POST /nodes` | `registerNode` | `201`, the node in `PROPOSED` |
| Nodes | `GET /nodes` | `listNodes` | `200 {items, nextCursor}`, by id, filters `type`, `region`, `status` |
| Nodes | `GET /nodes/{nodeId}` | `getNode` | `200`, the node with its current `operationalProfile` |
| Nodes | `PUT /nodes/{nodeId}/operational-profile` | `declareOperationalProfile` | `200`, the node with the new revision |
| Nodes | `POST /nodes/{nodeId}/activations` | `activateNode` | `200`, the node `ACTIVE` |
| Nodes | `POST /nodes/{nodeId}/suspensions` | `suspendNode` | `200`, the node `SUSPENDED` (body `{reason}`) |
| Nodes | `POST /nodes/{nodeId}/retirements` | `retireNode` | `200`, the node `RETIRED` |
| Lanes | `POST /lanes` | `openLane` | `201`, the lane `OPEN` |
| Lanes | `GET /lanes` | `listLanes` | `200 {items, nextCursor}`, filters `origin`, `destination` |
| Lanes | `POST /lanes/{laneId}/closures` | `closeLane` | `200`, the lane `CLOSED` |
| Service areas | `PUT /service-areas/{postalPrefix}` | `assignServiceArea` | `200`, the assignment at its new version |
| Service areas | `GET /service-areas/{postalPrefix}` | `resolveServiceArea` | `200`, the current assignment; `404 not-found` if unassigned |
| Regions | `POST /regions` | `defineRegion` | `201`, the region |
| Regions | `GET /regions` | `listRegions` | `200 {items, nextCursor}`, regions |
| Regions | `GET /regions/{regionId}` | `getRegion` | `200`, the region |
| Health | `GET /healthz`, `GET /readyz` | none (not in the spec) | `200`, or `503` on `/readyz` while draining or a dependency is down |

## Conventions in the contract

- **`Idempotency-Key`.** Required on every `POST` and on the two appending `PUT`s (`PUT /nodes/{nodeId}/operational-profile`, `PUT /service-areas/{postalPrefix}`); `400 idempotency-key-required` when absent, `400 idempotency-key-invalid` unless 1 to 128 printable ASCII characters. The key is global: the same key with an equal method, path and body replays the stored answer byte for byte with header `Idempotency-Replayed: true`; with a different request it is `409 idempotency-key-reused`. Keys live 24 hours. 2xx and 4xx outcomes are stored, 5xx never.
- **Versioning without ETags.** There is no `If-Match`. A profile carries `profileRevision` and a service area `assignmentVersion`; the caller sends the next number and a stale one is `409 stale-profile-revision` or `409 stale-assignment-version`. Profile revisions are immutable and kept.
- **Lists.** Keyset-paginated: nodes, lanes and regions come back by id ascending as `{items, nextCursor}` (`limit` 1-500, default 100, opaque `cursor`; `nextCursor` is absent on the last page), `{items: []}` when nothing matches. `400 invalid-limit`, `400 invalid-cursor`, and `400 unknown-query-parameter` for any parameter the collection does not declare. An unknown `type` or `status` filter is `422 validation-failed`. The MCP `list_regions` still wraps every region as `{regions}`.
- **Errors** are RFC 9457 `application/problem+json` with `type` set to the slug alone (not a URL), plus `title`, `status`, `detail` and `instance`. The slugs: `not-found`, `method-not-allowed`, `illegal-transition`, `concurrent-update`, `validation-failed`, `malformed-request-body`, `malformed-request-query`, `request-body-too-large`, `idempotency-key-required`, `idempotency-key-invalid`, `idempotency-key-reused`, `vendor-node-code-taken`, `operator-not-allowed-for-node-type`, `subtype-not-allowed`, `stale-profile-revision`, `incomplete-operational-profile`, `cutoff-outside-operating-hours`, `sort-dwell-not-allowed`, `inbound-limits-not-allowed`, `node-retired`, `lane-loop`, `lane-type-pair-not-allowed`, `lane-endpoint-not-active`, `duplicate-lane`, `not-a-delivery-station`, `stale-assignment-version`, `region-code-taken`, `unknown-region`, `dependency-unavailable` and `internal-error`. Status, trigger and remedy for each are on [Troubleshooting](/docs/operations/troubleshooting#rest-problem-types); the use case that raises each is on [Use cases](/docs/ddd/use-cases).
- **Limits.** Request bodies over 64 KiB are `413 request-body-too-large`; a NUL character in a body or query string is `400`.
- **Auth**: none. The fleet reverted REST, MCP and A2A auth on 2026-09-11, and `TestNoAuthMiddlewareReintroduced` fails CI if auth middleware returns under `internal/adapters/inbound`.
- **Events** are the Published Language: dispatch on the full `type`, ignore unknown types, deduplicate on the CloudEvents `id`, keep the highest `profileRevision` or `assignmentVersion` per aggregate.

## MCP

`cmd/mcp` serves the six read tools `get_node`, `list_nodes`, `list_lanes`, `resolve_service_area`, `get_region` and `list_regions` over Streamable HTTP. Each is annotated read-only, idempotent and closed-world; a test fails the build on a write-verb name. Errors come back as an `isError` tool result with text `<slug>: <detail>`. The input fields of every tool are on [MCP tools](/docs/mcp/tools).

## A2A

`cmd/a2a` is a message-only agent: each request is one JSON-RPC message whose first part (a data part, or a text part holding the same JSON) is `{"skill": "<id>", "arguments": {...}}`; the reply is one agent message holding a data part and a one-line text summary. Unknown fields are rejected. The Agent Card is served at `/.well-known/agent-card.json` (name `node-topology`, JSON-RPC transport, no streaming, no security schemes) and advertises the URL set in `A2A_AGENT_URL`.

| Skill id | Arguments | Answers |
| --- | --- | --- |
| `serving_station` | `postalPrefix` (required, matched exactly) | The assignment (`deliveryStationNodeId`, `regionId`, `assignmentVersion`) together with the station node and its current operational profile. |
| `find_nodes` | `type`, `regionId`, `status`, all optional and combined with AND | `{count, nodes}` by id ascending. |

```json
{"skill":"find_nodes","arguments":{"type":"DELIVERY_STATION","status":"ACTIVE"}}
```

Domain rejections are answered inside the message envelope as a `problem` object, never as a JSON-RPC error: `not-found` (404) for an unassigned prefix, `validation-failed` (422) for a bad `type` or `status`, `invalid-argument` (400) for a missing `postalPrefix`, unknown argument or NUL character, `malformed-skill-request` (400) for a message that is not a skill request, `unknown-skill` (400), and `internal-error` (500). The A2A routes are not in `apis/openapi.yaml`; `/a2a` and `/` are described only here and in [ADR 0011](/docs/adr/0011-a2a-inbound-adapter).
