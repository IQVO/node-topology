# Architecture decision records

The decisions of `node-topology`, in the order they were taken. The ADR bodies
are immutable; a changed decision gets a new ADR that supersedes the old one.
The *Status* column is copied from each ADR's own `## Status` section.

| ADR | Title | Status |
| --- | --- | --- |
| [0001](./0001-node-topology-bounded-context.md) | Node Topology is a bounded context of the retail network | Accepted (bootstrap) |
| [0002](./0002-aggregates-and-invariants.md) | the Node, Lane, ServiceArea and Region aggregates, their invariants and the assumptions the model left open | Accepted (2026-10-23) |
| [0003](./0003-no-consumed-contracts-and-ohs-reads.md) | no consumed contracts, Open Host Service reads, outbox publishing and versioned profile revisions | Accepted (2026-10-23) |
| [0004](./0004-cloudevents-envelope-and-type-catalogue.md) | CloudEvents envelope and type catalogue | Accepted (2026-10-23) |
| [0005](./0005-outbox-relay-and-attempt-cap.md) | the outbox relay, its attempt cap and the CloudEvents encoders | Accepted (2026-10-23) |
| [0006](./0006-http-adapter-idempotency-and-readiness.md) | the HTTP adapter, Idempotency-Key, problem types and readiness | Accepted (2026-10-23) |
| [0007](./0007-inbound-limits-on-the-operational-profile.md) | inbound limits on the operational profile | Accepted (2026-10-24) |
| [0008](./0008-mcp-server-adoption.md) | a read-only MCP server | Accepted (2026-10-24) |
| [0009](./0009-console-remote.md) | a web console remote (node_topology_mfe) | Accepted (2026-10-24) |
| [0010](./0010-fleet-observability-standard.md) | Adopt the fleet observability standard (logs, metrics, traces) | Accepted (2026-10-26) |
