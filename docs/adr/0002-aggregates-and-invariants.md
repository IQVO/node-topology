# ADR 0002: the Node, Lane, ServiceArea and Region aggregates, their invariants and the assumptions the model left open

## Status

Accepted (2026-10-23). Design source: retail-network-docs `model/model.yaml`,
context `node-topology` (aggregates, `node_types`, `lane_rules`) and the pack
pages `aggregate-design-canvas`, `use-cases` and `domain-events`.

## Context

The model gives four aggregate roots, each its own transaction boundary:
`Node`, `Lane`, `ServiceArea` and `Region`. Three invariants span aggregates
(vendor node code, open lane per triple, region code) and four more read a
second aggregate (lane endpoints, service-area station and region, node region).
The model is silent on a number of details; every one the implementation had to
decide is an **assumption** below, flagged `A<n>`, and is also reported to the
orchestrator as a model gap. The model is not edited here.

## Decision

### Layout and conventions

- `internal/domain/{shared,region,node,lane,servicearea}`. Pure Go: no I/O,
  `time.Now` and id generation are never called; `now` and ids are passed in.
  Dependency direction: `region` <- `node` <- `lane`, `servicearea`; everything
  may use `shared`.
- `shared` holds what more than one aggregate raises or implements:
  `ErrValidation` (problem `validation-failed`, 422), `ErrIllegalTransition`
  (problem `illegal-transition`, 409), `ErrInvalidSnapshot` (a persisted row that
  breaks an invariant; an internal error, never a client error) and the `Event`
  interface. `ErrNotFound` is NOT a domain error: it is the port sentinel
  `ports.ErrNotFound` of the application layer (step 2). `ErrConcurrentUpdate` is
  raised by the repository on a version mismatch, also not by the domain.
- Each aggregate has a constructor or command method per model command, a
  `Snapshot()` and a validating `Restore(Snapshot)`, and a `Version()` for
  optimistic concurrency. A command returns the raised events; `Version()` after
  the command is the version the events carry, and the repository stores with the
  expectation `stored version == Version()-1` (`0` for a create). Events are typed
  structs whose fields are exactly the model's `data` list (ADR 0004).
- The node-type table (`node_types`: operators, capabilities, subtypes) and the
  lane-pair table (`lane_rules.allowed`) are typed tables in the domain, each row
  pinned by a test against the model's values.

### Node

States `PROPOSED`, `ACTIVE`, `SUSPENDED`, `RETIRED`; transitions (command): `PROPOSED -> ACTIVE`
(ActivateNode), `ACTIVE -> SUSPENDED` (SuspendNode), `SUSPENDED -> ACTIVE`
(ActivateNode), `ACTIVE -> RETIRED` and `SUSPENDED -> RETIRED` (RetireNode).
`DeclareOperationalProfile` changes no state and is accepted in `PROPOSED`,
`ACTIVE` and `SUSPENDED`. A transition not in the table raises
`ErrIllegalTransition`, except any command on a `RETIRED` node, which raises
`ErrNodeRetired`.

| Invariant | Err | Enforced by |
| --- | --- | --- |
| operator allowed for the node type (parcel nodes are NETWORK-operated) | `ErrOperatorNotAllowedForNodeType` | `node.Register` |
| `vendorNodeCode` unique per vendor | `ErrVendorNodeCodeTaken` | repository unique index `(vendor_id, vendor_node_code)`; the use case pre-checks (A10) |
| profile revision strictly increases | `ErrStaleProfileRevision` | `Node.DeclareProfile` |
| FULFILLMENT_CENTER has at least one cutoff and `handlingTime > 0` before activation, and every later revision of an ACTIVE one keeps both | `ErrIncompleteOperationalProfile` | `Node.Activate`, `Node.DeclareProfile` |
| every day of a cutoff falls inside that day's operating hours | `ErrCutoffOutsideOperatingHours` | `NewOperationalProfile` |
| `regionId` names a defined Region | `ErrUnknownRegion` | `node.Register` (given the `*region.Region` the use case loaded; nil or a different id means unknown) |
| `subtype`, when given, is one of the type's subtypes | `ErrSubtypeNotAllowed` | `node.Register` |
| `sortDwellMinutes` only for SORTATION_CENTER and AIR_HUB | `ErrSortDwellNotAllowed` | `Node.DeclareProfile` |
| RETIRED is terminal | `ErrNodeRetired` | every command |

Capabilities are derived from the node type and never stored or accepted
(`DefaultCapabilitiesByType`). `AcceptsDemand()` is true only for an `ACTIVE` node
with the `OUTBOUND` capability (business decision 2).

Check order in `Register`: enumerations valid; operator allowed; remaining value
checks (id, vendor reference, time zone, location); region; subtype. In
`DeclareProfile`: retired; revision stale; profile shape and cutoff windows;
completeness of an ACTIVE fulfillment centre; sort dwell.

### Lane

States `OPEN`, `CLOSED`; transition `OPEN -> CLOSED` (CloseLane); no reopen.

| Invariant | Err | Enforced by |
| --- | --- | --- |
| origin and destination differ | `ErrLaneLoop` | `lane.Open` |
| (origin type, destination type) is in `lane_rules.allowed` | `ErrLaneTypePairNotAllowed` | `lane.Open` |
| both ends ACTIVE when opening | `ErrLaneEndpointNotActive` | `lane.Open` (a nil end counts as not active) |
| at most one OPEN lane per (origin, destination, mode) | `ErrDuplicateLane` | repository partial unique index; the use case pre-checks (A10) |

Order: values valid; loop; type pair; endpoints active. Closing an already
`CLOSED` lane raises `ErrIllegalTransition`.

### ServiceArea

One state `ASSIGNED` (initial); transition `ASSIGNED -> ASSIGNED` (AssignServiceArea).
The first assignment creates the aggregate; later ones call `Reassign`.

| Invariant | Err | Enforced by |
| --- | --- | --- |
| the assigned node is an ACTIVE DELIVERY_STATION | `ErrNotADeliveryStation` | `servicearea.Assign`, `Reassign` (a nil station counts as not one) |
| assignment version strictly increases | `ErrStaleAssignmentVersion` | `Reassign` (and `Assign`: version must be >= 1) |
| the region is a defined Region and equals the station's region | `ErrUnknownRegion` | `Assign`, `Reassign` (given the `*region.Region` the use case loaded) |

Order: prefix and version valid; station; version; region. The region of the
prefix is taken from the station, never from the caller.

### Region

One state `DEFINED` (initial); no transition.

| Invariant | Err | Enforced by |
| --- | --- | --- |
| region code unique | `ErrRegionCodeTaken` | repository unique index on `code`; the use case pre-checks (A10) |

### Assumptions (the model is silent)

- **A1 Time zone.** The domain checks the *shape* of an IANA name (`Area/Location`,
  letters, digits, `_`, `-`, `+`), not that the host tz database knows it: resolving
  it is I/O and belongs to the HTTP adapter if wanted.
- **A2 Vendor reference.** A `VENDOR` node needs a `vendorRef` with a non-blank
  `vendorId` and `vendorNodeCode`; a `NETWORK` node must not carry one. Violations
  are `ErrValidation`. Operator is checked against the type first, so a NETWORK
  operator on a fulfillment centre is `ErrOperatorNotAllowedForNodeType`, not a
  missing reference.
- **A3 Profile time zone.** `Node.timezone` is fixed at registration. A profile
  declares its own `timezone` and the domain does not require it to equal the
  node's. (Model gap: should it?)
- **A4 Cutoff window.** A day's hours are same-day `start < end` (no overnight
  shift); a cutoff is inside when `start <= localTime <= end` (both ends
  inclusive). A cutoff without `days` applies to every operating day; if the node
  has no operating day at all, that cutoff is outside the operating hours
  (`ErrCutoffOutsideOperatingHours`). Two cutoffs with the same `localTime` and an
  overlapping effective day set are `ErrValidation` (one pickup departure per
  cutoff). Holidays do not remove a weekday from the cutoff check.
- **A5 Completeness scope.** Only a FULFILLMENT_CENTER needs cutoffs and a
  handling time above zero. Any other node type may be activated with no profile
  at all, and a profile may carry zero cutoffs. A PROPOSED or SUSPENDED
  fulfillment centre may hold an incomplete profile; re-activation re-checks the
  current one.
- **A6 Declared by.** `declaredBy` `VENDOR` is refused (`ErrValidation`) for a
  NETWORK-operated node; `NETWORK_OPS` may declare any node's profile.
- **A7 Profile values.** `profileRevision`, `handlingTimeMinutes`,
  `sortDwellMinutes`, `maxOrders`, throughput `value`, `nominalTransitMinutes` and
  `assignmentVersion` are bounded above by the literal `2147483647` (Postgres
  `integer`); `handlingTimeMinutes` and `sortDwellMinutes` are >= 0; `maxOrders`
  and the cap are >= 1; `throughputCap.unit` must be `ORDER`; `holidays` are
  ISO `YYYY-MM-DD` dates, stored sorted, duplicates refused.
- **A8 Suspend reason.** `reason` is required (non-blank) on SuspendNode.
- **A9 Wire types.** `data` is camelCase; `subtype`, `vendorId` and
  `vendorNodeCode` of `NodeRegistered` are JSON `null` when not applicable;
  `sortDwellMinutes`, cutoff `days` and `maxOrders` are omitted when unset
  (ADR 0004, `apis/asyncapi.yaml`).
- **A10 Cross-aggregate uniqueness.** One aggregate cannot know its siblings, so
  `ErrVendorNodeCodeTaken`, `ErrDuplicateLane` and `ErrRegionCodeTaken` are
  *declared* in the domain and *raised* by the repository when its unique index
  is violated (the use case pre-checks for a friendlier path). The model names
  the indexes in the canvas.
- **A11 Identifiers.** Ids (`nodeId`, `laneId`, `regionId`) are opaque strings the
  application mints: 1 to 64 characters from `A-Z a-z 0-9 . _ : -`, starting with
  a letter or digit. `vendorId` and `vendorNodeCode` are trimmed non-blank strings
  of at most 100 characters. A postal prefix is 1 to 16 characters from `A-Z 0-9 -`
  (uppercase only, never normalised) and is matched exactly: the model says
  nothing about overlapping prefixes such as `01` and `013`, so none is resolved.
- **A12 Region.** `code` is 1 to 16 characters from `A-Z 0-9 _ -`; `name` is
  non-blank, trimmed, at most 120 characters.
- **A13 Location.** `postalCode` is non-blank, at most 20 characters; `countryCode`
  is two uppercase letters; `lat` in [-90, 90] and `lon` in [-180, 180], both or
  neither.
- **A14 Lane modes.** The model gives modes `GROUND` and `AIR` but no rule tying a
  mode to a type pair, so any mode is allowed on any allowed pair. A closed lane is
  never reopened; a new lane gets a new id and a new `LaneOpened`.
- **A15 Reassignment.** `Reassign` may name the same station again under a higher
  version (it still raises `ServiceAreaAssigned`). Closing a lane and
  reassigning a prefix do not require the endpoints to still be ACTIVE
  (`SuspendedEndpointsUnusable` leaves them as they are).
- **A16 Unknown references.** An unknown node or station id is `ErrNotFound`
  (404) decided by the use case before the domain runs, as the OpenAPI says; the
  domain's `ErrUnknownRegion` covers a region that does not exist for a node body
  or a station.

## Consequences

- Every invariant above has a table-driven test, including the illegal order
  combinations; the node-type and lane-pair tables are pinned row by row.
- The use cases of step 2 must load the sibling aggregate they pass in
  (`*region.Region`, `*node.Node`) and translate a missing one to `ErrNotFound`.
- The three repository-raised errors need the Postgres unique indexes of step 3
  and a repocontract test for each.

## Amendment (step 2, application layer): assumptions A17-A24

Step 2 adds `internal/application/{ports,views,usecases,repository}` and the
test doubles in `internal/adapters/outbound/memory`. The model and OpenAPI are
silent on the points below; each is an assumption and is reported as a model gap.

- **A17 Retry on conflict.** ADR 0003 decision 4 says a version mismatch is
  retried once by reloading. It is implemented in the use cases: when a unit of
  work fails with `ports.ErrConcurrentUpdate`, the whole attempt (load, domain
  call, save, outbox append) runs once more on fresh state; a second conflict is
  returned as `ErrConcurrentUpdate` (409). A create re-mints its id on the retry.
- **A18 Save contract.** `Save` expects `stored version == aggregate.Version()-1`
  (a create expects the id to be free). Creating an id that exists, and updating
  a row that no longer exists, are both `ErrConcurrentUpdate`. The repository
  raises `ErrVendorNodeCodeTaken`, `ErrDuplicateLane` and `ErrRegionCodeTaken`
  from its unique indexes after the version check.
- **A19 Outbox port.** `EventOutbox.Append(ctx, events ...shared.Event)` takes
  the domain events; the adapter encodes the CloudEvents envelope and mints its
  `id` at append (step 4). The use case calls it in the same `UnitOfWork.Do` as
  the aggregate saves, so both commit or roll back together.
- **A20 Check order in commands.** Unknown node, station or lane ids are
  `ErrNotFound` first (A16); then the domain's own checks; the cross-aggregate
  pre-check (vendor code, open lane, region code) runs after them, so an invalid
  command is never reported as a taken code. An unknown region id on
  `RegisterNode` or on a station's region is `ErrUnknownRegion` (422), not 404.
- **A21 Lists.** `ListNodes` and `ListLanes` return every match ordered by id
  ascending, as an empty list when nothing matches, with no pagination (the
  model sizes it at about 10^3 nodes and 10^4 lanes). A `type` or `status`
  filter that is not an enumeration value is `ErrValidation` (422).
- **A22 Profile revisions.** `NodeRepository.Save` appends the profile revision
  row (its comment tells an adapter how); `NodeRepository.ProfileRevisions`
  reads them back, oldest first, so ADR 0003's "earlier revisions stay queryable
  for audit" is testable by the repository contract. No REST operation exposes
  it yet.
- **A23 Regions are write-only over REST.** The OpenAPI has `defineRegion` and no
  `getRegion` or `listRegions`, so there is no use case for them and a client
  cannot discover a `regionId` it did not create. Model gap: add the reads, or
  say that regions are provisioned out of band.
- **A24 Assignment version below 1.** ADR 0002's table lists `ErrStaleAssignmentVersion`
  for a first assignment whose version is below 1; the domain raises
  `ErrValidation` (`assignmentVersion must be between 1 and 2147483647`), and the
  use case passes it through. The table row is read as "stale" for a
  reassignment only.
