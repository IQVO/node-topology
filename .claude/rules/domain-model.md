# Domain model: ubiquitous language, aggregates, events, use cases

Source of truth: warehouse-docs `network/model/model.yaml` (context `node-topology`).
Invariants, transitions and every assumption the model left open are in
`docs/adr/0002-aggregates-and-invariants.md` (assumptions A1-A42).

## Ubiquitous Language (use these exact names)

- **Node** — a building of the network (fulfillment centre, cross-dock, returns
  centre, sortation centre, delivery station, air hub). Identified by `nodeId`
  (`node-<uuid>`). Capabilities are derived from its type and never stored.
- **Node type / subtype / operator** — `FULFILLMENT_CENTER`, `INBOUND_CROSS_DOCK`,
  `RETURNS_CENTER`, `SORTATION_CENTER`, `DELIVERY_STATION`, `AIR_HUB`; a
  fulfillment-centre subtype; `NETWORK` or `VENDOR`. Parcel nodes are
  network-operated only.
- **Operational profile** — the declared opening hours per weekday, holidays,
  handling time, cutoffs, throughput cap and (sorting nodes only) sort dwell of a
  node. Versioned: every `DeclareOperationalProfile` appends a revision, a stored
  revision is never edited.
- **Cutoff** — a local time by which packages are ready for handover; one pickup
  departure per cutoff.
- **Lane** — a directed, mode-specific (`GROUND`, `AIR`) connection between two
  ACTIVE nodes with a nominal transit time. Only the pairs of `lane_rules.allowed`.
- **ServiceArea** — which ACTIVE delivery station serves a postal prefix; matched
  exactly, never normalised. Identified by the prefix; carries an
  `assignmentVersion`.
- **Region** — a partition of the network, unique by `code`.
- **Vendor node code** — the vendor's own code of a node, unique per vendor.

## Aggregates

- **Node** (`internal/domain/node`): states `PROPOSED`, `ACTIVE`, `SUSPENDED`,
  `RETIRED` (terminal); operator allowed for the type, profile revision strictly
  increasing, completeness of a fulfillment centre before activation, cutoffs
  inside the operating hours, sort dwell only for sorting nodes.
- **Lane** (`internal/domain/lane`): `OPEN` -> `CLOSED`, no reopen; no loop, an
  allowed type pair, both ends ACTIVE, one OPEN lane per (origin, destination, mode).
- **ServiceArea** (`internal/domain/servicearea`): the assigned node is an ACTIVE
  delivery station, the assignment version strictly increases, the region comes
  from the station.
- **Region** (`internal/domain/region`): code unique.
- `internal/domain/shared`: `Event`, `Header`, `ErrValidation`,
  `ErrIllegalTransition`, `ErrInvalidSnapshot`, id and text checks.

Every aggregate has `Snapshot()`, a validating `Restore(Snapshot)` and an
optimistic `Version()`; `now` and ids are passed in. `ErrNotFound` and
`ErrConcurrentUpdate` are port errors (`internal/application/ports/errors.go`),
not domain errors. The domain imports nothing outside `internal/domain`.

## Domain events

All nine are integration events on `retailnetwork.node-topology.events`
(`internal/domain/*/events.go`; wire shape in `apis/asyncapi.yaml`):

- `NodeRegistered` — Register. `NodeOperationalProfileDeclared` — DeclareProfile
  (the whole new revision). `NodeActivated`, `NodeSuspended`, `NodeRetired`.
- `LaneOpened`, `LaneClosed`.
- `ServiceAreaAssigned` — Assign and Reassign.
- `RegionDefined`.

## Key use cases (`internal/application/usecases`)

One per command and query; commands run in ONE `ports.UnitOfWork` (load, domain
call, save, outbox append) and retry once on `ErrConcurrentUpdate`.

- `RegisterNode`, `DeclareOperationalProfile`, `ActivateNode`, `SuspendNode`,
  `RetireNode` (`nodes.go`); `OpenLane`, `CloseLane` (`lanes.go`);
  `AssignServiceArea` (`servicearea.go`); `DefineRegion` (`regions.go`).
- Queries (`queries.go`): `GetNode`, `ListNodes`, `ListLanes`,
  `ResolveServiceArea`, `GetRegion`, `ListRegions`. Read DTOs are in
  `internal/application/views`; filters in `internal/application/repository`.
- Ports (`internal/application/ports/ports.go`) are interfaces only; the
  memory test doubles in `internal/adapters/outbound/memory` are test doubles and
  must pass the same suite as Postgres (`internal/adapters/outbound/repocontract`).
