# ADR 0009: a web console remote

## Status

Accepted (2026-10-24).

## Context

The fleet's operators read every bounded context through the warehouse-console
shell, a Module Federation host that lazy-loads one remote per context. Every
context that exposes reads to operators ships such a remote
(task-interleaving, slotting-optimization, inbound-receiving, ...); node-topology
had none, so browsing the network's nodes, lanes, service areas and regions
meant raw REST calls.

## Decision

- **`web/` is the `node_topology_mfe` Module Federation remote.** It exposes
  exactly `./App` (a default export, no props); the console lazy-loads it as
  `node_topology_mfe/App` and mounts it under its own `/node-topology/*` splat
  route, providing the `BrowserRouter`, the design tokens and the runtime
  config. It also runs standalone on port 4017 (`npm run dev` /
  `npm run preview`, `strictPort`) without the shell.
- **A small, REAL, read-only UI: three screens over this context's own GET
  routes.** A node registry browser (`GET /nodes` with the type, region and
  status filters; a row links to the node's page with its current operational
  profile), a lane browser (`GET /lanes` with the origin and destination
  filters) and a region browser (`GET /regions`). Nothing writes: the remote
  has no mutation call, no `Idempotency-Key`, no POST/PUT anywhere.
- **Runtime endpoint resolution, not build-time.** `src/config.ts` turns
  `window.__WAREHOUSE_CONFIG__.apiOrigin` (published by the shell from
  `/config.json`) into `${apiOrigin}/api/node-topology`, the only base this
  remote ever calls; a production build without an origin refuses to render
  rather than falling back to a developer port.
- **The deployment shape of the fleet.** `web/Dockerfile` is the two-stage
  uikit named-context build (node:22-bookworm-slim; `@warehouse/ui-kit` is the
  `file:../../warehouse-ui-kit` sibling checkout, built first and consumed from
  its `dist`), served by nginx-unprivileged with the fleet's `nginx.conf`
  (unhashed `remoteEntry.js` and `index.html` never cached; immutable hashed
  assets; SPA fallback; `/healthz` for the probes). Vite's `base` is
  `/mfes/node-topology/` on build (STATIC object config, never a callback, so
  configs stay mergeable) and the web gateway — not this chart — owns that
  public path.
- **Chart: an optional `frontend` component, off by default.** Its own
  Deployment + ClusterIP Service (`component=frontend`), no database, no
  Kafka, no routing in this chart. CI grows a `web` job (ui-kit checked out as
  a sibling; oxlint, `tsc -b`, vitest, `vite build`).

## Consequences

- Operators browse the topology without REST tooling; agents keep using the
  MCP server of ADR 0008 against the same use cases.
- The remote is deployable as one image in more than one environment: only
  `/config.json` changes between them.
- The console shell must carry the matching federation entry
  (`node_topology_mfe` at `./App`, remote entry under `/mfes/node-topology/`)
  — a pinned host/remote contract, changed on both sides together or not at
  all.
