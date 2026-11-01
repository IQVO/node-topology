---
id: tools
title: MCP tools
sidebar_label: MCP tools
description: "The six read-only MCP tools of Node Topology: input fields, outputs, annotations and error behaviour."
---

# MCP tools

`cmd/mcp` is a separate binary that serves the Model Context Protocol over **Streamable HTTP only** (official Go SDK), at `/mcp` and `/` on `MCP_ADDR` (default `:8090`); through Kong the path is `/api/mcp/node-topology`. The server identifies itself as `node-topology-mcp` version `1.0.0`. It is read-only by construction ([ADR 0008](/docs/adr/0008-mcp-server-adoption)): the adapter's `Deps` hold six read use cases and no write use case, a test fails the build on a write-verb tool name, and the binary never starts the outbox relay or dials Kafka. There is no authentication; reachability is the cluster boundary.

:::note How this page was produced
The tool list below is read from `internal/adapters/inbound/mcp/tools.go` (`registerTools`) and the input structs in the same file. It was not captured from a live `tools/list` call.
:::

Every tool carries the same annotations: `readOnlyHint: true`, `idempotentHint: true`, `openWorldHint: false`. There is no write tool. Registering nodes, declaring profiles, lifecycle transitions, lanes, service areas and regions are REST operations.

| Tool | Read or write | Input fields | Output |
| --- | --- | --- | --- |
| `get_node` | read-only | `nodeId` (string, required) | one node |
| `list_nodes` | read-only | `type`, `regionId`, `status` (all optional strings, combined with AND) | `{nodes: [...]}` |
| `list_lanes` | read-only | `origin`, `destination` (optional node ids, combined with AND) | `{lanes: [...]}` |
| `resolve_service_area` | read-only | `postalPrefix` (string, required, matched exactly) | one service area |
| `get_region` | read-only | `regionId` (string, required) | one region |
| `list_regions` | read-only | none | `{regions: [...]}` |

## Tools

### `get_node`

Reads one node with its current operational profile. Input `nodeId`, for example `node-3f6a`. Output: `nodeId`, `nodeType`, `subtype`, `operator`, `vendorRef{vendorId, vendorNodeCode}`, `regionId`, `timezone`, `location{postalCode, countryCode, lat, lon}`, `capabilities{outbound, inbound, returns, sort, lastMile, air}`, `status`, `operationalProfile{profileRevision, timezone, operatingHoursByDay, holidays, handlingTimeMinutes, cutoffs, throughputCap, sortDwellMinutes, declaredBy, declaredAt}` and, when set, `activatedAt`, `suspendedAt`, `suspendReason`, `retiredAt`. Optional fields are omitted when unset. An unknown id fails with `not-found`.

### `list_nodes`

Lists nodes by id ascending. `type` is one of `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`, `RETURNS_CENTER`, `SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB`; `regionId` filters one region; `status` is one of `PROPOSED`, `ACTIVE`, `SUSPENDED`, `RETIRED`. Omitted means no filter on that field. No match is an empty list. A `type` or `status` that is not an enumeration value fails with `validation-failed`. Each node carries its current profile. Note the REST query parameter is `region`; the tool input is `regionId`.

### `list_lanes`

Lists directed lanes by id ascending, optionally only those leaving `origin` and/or arriving at `destination`. Open and closed lanes are both listed: each lane has `laneId`, `originNodeId`, `destinationNodeId`, `mode` (`GROUND` or `AIR`), `nominalTransitMinutes`, `status` (`OPEN` or `CLOSED`) and `closedAt` when closed.

### `resolve_service_area`

Resolves which delivery station serves a postal prefix, for example `01310`. Returns `postalPrefix`, `deliveryStationNodeId`, `regionId` and `assignmentVersion`. An unassigned prefix fails with `not-found`; the match is exact and never normalised.

### `get_region`

Reads one region by id: `regionId`, `code`, `name`. An unknown id fails with `not-found`.

### `list_regions`

Lists every region by id ascending. Regions are few and not paginated; none defined is an empty list. The output is wrapped as `{regions}` so structured content is always an object; the REST route returns a bare array.

## Errors

A failing tool returns an `isError` tool result, never a transport failure. The text is `<slug>: <detail>` with the same slug the REST adapter uses: `not-found`, `validation-failed`, or `internal-error: an unexpected internal error occurred` (the real cause is logged as `mcp tool failed with an unexpected error`, with the trace id of the call).

## Observability and operations

The MCP router uses the same middleware order as REST: request id, an `otelchi` server span named after the route, the request-duration histogram, the access log, then the recoverer. `GET /healthz` is open. Variables, ports and the shared Postgres are on [Configuration](/docs/operations/configuration); the interface table is on the [API overview](/docs/api-reference).
