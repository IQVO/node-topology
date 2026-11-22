# ADR 0020: every problem type has a replay class; a retryable refusal is not stored under its Idempotency-Key

## Status

Accepted (2026-11-19). Supersedes the sentences "a 4xx is stored and replayed" of ADR 0006 point 5 and its Consequences, of ADR 0012
and of ADR 0002 A38, and the matching sentence of the `Idempotency-Key` text of `apis/openapi.yaml` (the Decision text
of those records is not rewritten: each carries a dated pointer here). Adopts the fleet rule decided by the owner on
2026-11-18 (`.claude/rules/fleet/idempotency-and-outbox.md`, `docs/idempotency-replay-classes.md` of
warehouse-harness-template). No route, status code, problem slug, header, event or table changes: a refusal that was
replayed for 24 hours is, for ten of the 28 problem types, now answered again by running the request.

## Context

`RequireIdempotencyKey` (`internal/adapters/inbound/http/idempotency.go`) claims the key row inside the request
transaction (`claim`: purge rows older than 24 hours, then `INSERT ... ON CONFLICT (key) DO NOTHING`), runs the handler
in a savepoint of that transaction bound into the context (`TxBinder`, `pgtx.With`) and, until now, stored EVERY answer
below 500: a 2xx with its writes, a 4xx with the writes rolled back (`settle`: "a refused request leaves no trace"), so
the same key and body replayed the refusal for 24 hours. It guards the 11 routes registered through `Server.keyed`
(`handlers_nodes.go` 13, 16-19; `handlers_other.go` 14, 17, 18, 22, 27).

That is wrong for a refusal the same request could pass a moment later:

- `409 concurrent-update` (another request moved the same node first and `transact` retried the attempt once,
  `writer.go` 70-89) stayed the answer for 24 hours.
- `422 unknown-region`, `409 lane-endpoint-not-active`, `409 not-a-delivery-station` and the like depend on the state of
  another aggregate that another request changes.
- vendor-connectivity calls `POST /nodes` and `PUT /nodes/{nodeId}/operational-profile` with the submission id as the
  key and retries until a terminal outcome: a stored refusal that a retry could pass stuck for the window.

The fleet decision: a retried request is answered from the store only when its first answer is final for that request.
Each problem type carries exactly one class, declared on its catalogue entry; the middleware reads the class and holds
no list of slugs and no status range beyond 2xx and 5xx.

## Decision

1. **The class is a field of the catalogue entry.** `problemCatalogue` (`internal/adapters/inbound/http/problems.go`)
   is a slice of `{err, p, replay}`; `replay` is `replayFinal`, `replayOutcome` or `replayRetryable`
   (`replay_class.go`), and so is the `replay` field of `detailError`, the adapter's own typed errors. The zero value is
   `replayUnset`; there is no runtime default class. A catalogue entry that declares none, a 5xx entry that is not
   RETRYABLE, or one slug declared with two classes fails `TestEveryProblemTypeDeclaresAReplayClass`; this ADR's table is
   held equal to the catalogue by `TestTheADRDeclaresTheClassOfEveryProblem` and the RETRYABLE list of the OpenAPI text
   by `TestTheSpecListsTheRetryableProblemTypes`. The adapter-driven fleet test
   (`idempotency_replay_classes_integration_test.go`, the harness template rendered for `/regions`) walks the same
   catalogue on a real Postgres.
2. **The error writer marks the capture.** `writeError` tells the middleware's capturing writer the class of the entry it
   answers with (`replayMarker`, `capture.MarkReplay`). A handler that answers without a catalogue problem leaves the
   class unset.
3. **The middleware acts on the verdict, nothing else** (`capture.verdict`):

   | answer | verdict |
   | --- | --- |
   | 2xx | stored, the handler's writes commit with the key row and the stored answer (unchanged) |
   | 5xx and a panic | the whole transaction rolls back, key row included, nothing stored (unchanged) |
   | FINAL | the savepoint rolls back, the answer is stored and replayed (unchanged) |
   | OUTCOME | the savepoint COMMITS, the answer is stored in the same transaction (new, no entry uses it) |
   | RETRYABLE | released exactly as the 5xx branch: `tx.Rollback`, key row included, nothing stored (new) |
   | no class (anything else) | released like RETRYABLE: nothing may be stored without a class |

   The released answer is flushed to the client unchanged, after the rollback, and never carries `Idempotency-Replayed`.
   A 2xx, a 5xx and a FINAL refusal are byte-identical to before.
4. **Releasing a RETRYABLE key means the request is forgotten.** The same body under the same key runs again; a
   different body under the same key is a NEW request, not `idempotency-key-reused`: nothing is left to compare against.
   The 24-hour purge of `claim` rolls back with the released transaction and runs again on the next keyed write; a replay
   can only follow a `claim`, so no stored answer is replayed past its 24 hours.
5. **A concurrent duplicate while the first ends RETRYABLE.** The copy blocks on the first's `INSERT ... ON CONFLICT`
   (Postgres serialises on the primary-key unique index until the first transaction resolves). The first rolls back, the
   row is gone, the blocked insert succeeds: the copy is then a FRESH request and runs the handler itself. It never
   replays a released refusal and never runs alongside the first. Copies that were waiting behind it replay the answer of
   that second run. `TestACopyWaitingOnARetryableFirstRunsTheHandlerItself`: 6 copies, 1 refusal, 1 fresh 201, 4 replayed
   201s, the handler ran twice and never at once.
6. **No OUTCOME problem type exists in this service** (the only class that needs a use case to commit and then refuse),
   by reading every use case behind a keyed route (`origin/develop` at 64cbf90): each refuses BEFORE its first write and
   runs inside one `transact` unit of work.
   - `RegisterNode.register` (`nodes.go` 43-72): `optionalRegion` 45, `servingStation` 49, `node.Register` 53,
     `checkVendorCodeFree` 61 all return before `Nodes.Save` 64; `publish` 67 comes after it and only fails as a 5xx.
   - `mutateNode` (`writer.go` 93-114), behind `DeclareOperationalProfile`, `ActivateNode`, `SuspendNode` and
     `RetireNode` (`nodes.go` 108-152): `Nodes.Get` 98 and the aggregate method `apply` 102 refuse before `Nodes.Save` 106.
   - `OpenLane` (`lanes.go` 29-59): `Get` 30 and 34, `lane.Open` 38, `FindOpen` 44-51 before `Lanes.Save` 52; `CloseLane`
     (70-87), `DeclareLaneEmission` (104-120) and `DeclareLaneCost` (138-154): `Get` and the aggregate method before `Save`.
   - `DefineRegion` (`regions.go` 25-47): `region.Define` 26, `FindByCode` 32-39 before `Regions.Save` 40.
   - `AssignServiceArea` (`servicearea.go` 45-69, `assign` 100-113): every refusal (46, 50, 54, 58) before `ServiceAreas.Save` 62.
   A `Save` that fails on a unique index (`translateWrite`, `postgres/errors.go` 40-55) wrote nothing. Structurally,
   `settle` rolls the savepoint back for every 4xx, so no refusal could commit state before this ADR either. The OUTCOME
   verdict is nevertheless implemented and proven against Postgres
   (`TestAnOutcomeRefusalCommitsItsWritesWithItsStoredAnswer`, the capture marked by hand), so the first refusal that does
   commit gets its writes kept instead of silently rolled back. Its entry needs the cited lines of its use case.
7. **MCP and A2A write nothing** (ADR 0008 and 0013: read-only tools; ADR 0011: read-only skills): no store of their own,
   nothing to classify.
8. **Retention** is enforced as the rule asks: `claim` purges every row older than 24 hours on each keyed write
   (`TestAnExpiredKeyIsANewKey`).

### The classes

The question per entry: did the command commit state before refusing (OUTCOME)? Is the answer a function of the request
alone (FINAL)? Otherwise RETRYABLE. When unsure, RETRYABLE: the refusal rolled everything back, so running it again costs
one transaction and cannot pin a false answer for 24 hours. The status and the slug do not decide the class. Facts used
below: no statement deletes a node, lane, region or service area (the only DELETEs are the key purge and a trigger that
REFUSES them on `node_profile_revisions`); no use case changes a node's type, operator, vendor reference or region, a
region's code, or a service area's version downwards; a node's status moves only along `node.transitions`
(`types.go` 147-153), where RETIRED has no way out.

| Problem type | Class | Status | Reason (code of `origin/develop` at 64cbf90) |
| --- | --- | --- | --- |
| `malformed-request-body` | FINAL | 400 | `decodeJSON` (`response.go` 43-63), `errMalformedRequest`, `errNULBody`: the body is not valid JSON for the operation; a function of the request |
| `malformed-request-query` | FINAL | 400 | `errNULQuery` (`nul.go` 30): a NUL in the query; answered before the middleware, listed because it goes through the same writer |
| `request-body-too-large` | FINAL | 413 | `errTooLarge`: the middleware answers it itself before claiming the key (`idempotency.go` 91-94), the handler's copy (`decodeJSON` 48) is unreachable; a function of the request |
| `validation-failed` | FINAL | 422 | every source is the request's own fields (`checkEnums`, `shared.Invalid` in `node.Suspend` 47, `lane.Open`, `servicearea.assign` 101-103, `region.Define`, the profile shape checks `profile_validate.go`) or reads the node's operator, which never changes (`checkProfileFits` `node_restore.go` 106-108) (J5) |
| `vendor-node-code-taken` | FINAL | 409 | `checkVendorCodeFree` (`nodes.go` 76-90) over the unique index `nodes_vendor_ref_key` (`0001` line 48, not partial on status); a node is never deleted and its vendor reference never changes, so once taken it stays taken |
| `stale-profile-revision` | FINAL | 409 | `DeclareProfile` (`node_lifecycle.go` 75-77): `d.Revision <= n.ProfileRevision()`; revisions are append-only and only increase (`node_repository.go` 22), so a stale revision never becomes current |
| `node-retired` | FINAL | 409 | `guard` (`node_lifecycle.go` 11-14) and `DeclareProfile` 72-74: RETIRED is terminal, `transitions` (`types.go` 147-153) has no move out of it and nothing deletes a node (J1) |
| `operator-not-allowed-for-node-type` | FINAL | 422 | `node.Register` (`node.go` 112-114): the type and operator named in the request |
| `pickup-point-postal-code-not-labellable` | FINAL | 422 | `node.Register` (`node.go` 118-120): the postal code of the request |
| `subtype-not-allowed` | FINAL | 422 | `node.Register` (`node.go` 132-134): the type and subtype of the request |
| `cutoff-outside-operating-hours` | FINAL | 422 | `checkCutoffs`/`checkWindow` (`profile_validate.go` 155-186): the hours and cutoffs of the request |
| `sort-dwell-not-allowed` | FINAL | 422 | `checkProfileFits` (`node_restore.go` 112-114): the request's dwell against the node's type, which never changes |
| `inbound-limits-not-allowed` | FINAL | 422 | `DeclareProfile` (`node_lifecycle.go` 80-82): the request's limits against the node's type, which never changes |
| `lane-loop` | FINAL | 422 | `lane.Open` (`lane.go` 163-165): origin and destination ids of the request are equal |
| `lane-type-pair-not-allowed` | FINAL | 422 | `lane.Open` (`lane.go` 166-168): `PairAllowed` over the types of the two nodes, which never change (J6) |
| `stale-assignment-version` | FINAL | 409 | `ServiceArea.assign` (`servicearea.go` 110-112): the version only moves forward (`s.assignment` is set on success only) and a service area is never deleted |
| `carrier-options-invalid` | FINAL | 422 | `checkDelivery` (`carriers.go` 70-105): mode, count, carrier ids, costs and transit of the request; the handoff nodes are a separate slug |
| `region-code-taken` | FINAL | 409 | `DefineRegion` (`regions.go` 32-39) over `regions_code_key` (`0001` line 15); a region is never deleted and its code never changes (J7) |
| `not-found` | RETRYABLE | 404 | the node, lane or station named in the request or by it (`Nodes.Get` `writer.go` 98, `nodes.go` 163, `lanes.go` 30 and 34, `servicearea.go` 46 and 87, `Lanes.Get` `lanes.go` 71, 105 and 139). Ids are minted by this service (`ids.UUID`) and nothing deletes, but the slug is also the answer for a REFERENCED node (a lane's ends, a pickup point's station, a service area's station and handoffs) and for the read-port error `ports.ErrNotFound`; one slug, one class: when unsure RETRYABLE (J2) |
| `illegal-transition` | RETRYABLE | 409 | the aggregate is not in the state the command needs yet: `Node.guard` (`node_lifecycle.go` 15-17; suspend of a PROPOSED node, retire of a PROPOSED one, activate of an ACTIVE one: another request activates or suspends it), `Lane.Close` (`lane.go` 244) and the declarations (264, 287) |
| `concurrent-update` | RETRYABLE | 409 | the optimistic lock lost the race (`translateWrite` `errors.go` 52, `errStaleVersion` 61) after `transact` ran the attempt twice (`writer.go` 81-83): the next attempt reads the new version and passes |
| `incomplete-operational-profile` | RETRYABLE | 409 | `Activate` of a FULFILLMENT_CENTER (`node_lifecycle.go` 31-33) reads the node's current profile, which another request declares; `checkProfileFits` 109-111 reads the node's status (J3) |
| `serving-station-not-allowed` | RETRYABLE | 422 | `checkServingStation` (`node.go` 197-217): lines 200 and 205 are a function of the request, but 211 and 214 read the status and type of the serving station, which another request can activate (J4) |
| `lane-endpoint-not-active` | RETRYABLE | 409 | `lane.Open` (`lane.go` 169-173): an end that is PROPOSED or SUSPENDED becomes ACTIVE |
| `duplicate-lane` | RETRYABLE | 409 | `OpenLane` (`lanes.go` 44-51) over the PARTIAL unique index `lanes_one_open_key ... WHERE status = 'OPEN'` (`0001` line 105): `CloseLane` frees the combination, so the same request passes afterwards (J8) |
| `not-a-delivery-station` | RETRYABLE | 409 | `ServiceArea.assign` (`servicearea.go` 107-109): the station must be an ACTIVE delivery station; its status changes |
| `handoff-node-not-usable` | RETRYABLE | 409 | `checkHandoffs` (`carriers.go` 118-131): each handoff node must be ACTIVE; its status changes |
| `unknown-region` | RETRYABLE | 422 | `node.Register` (`node.go` 129-131) with the region `optionalRegion` loaded (`writer.go` 131-138), and `ServiceArea.assign` (`servicearea.go` 113-115): a referenced fact that is not known yet; no foreign key is checked when the region is named (J9) |

Problems the capture never holds (not catalogue entries, never stored): `idempotency-key-required` and
`idempotency-key-invalid` (400) and `idempotency-key-reused` (409) are written by the middleware itself, before or instead
of the handler (`keyFrom`, `replay`); `internal-error` (500) and `dependency-unavailable` (503, `/readyz`) are 5xx;
`method-not-allowed`, the route `not-found` (a path outside the router, or a NUL in the path, `rejectNUL`) and
`invalid-limit`, `invalid-cursor` and `unknown-query-parameter` belong to routes that are not keyed. 28 distinct slugs in
26 catalogue entries and 3 adapter errors: 18 FINAL, 10 RETRYABLE, 0 OUTCOME.

### Judgement calls

- **J1 `node-retired` is FINAL.** RETIRED is terminal by construction and no code path reopens or deletes a node, so the
  same request can never pass. It is the one terminal-state refusal with a clean slug: `illegal-transition` stays
  RETRYABLE because the same slug also answers moves that other requests make possible.
- **J2 `not-found` is RETRYABLE, not FINAL.** The owner's rule allows FINAL for a 404 on the command's own target when
  the ids are minted here and nothing is deleted. Both hold, and a mistyped id answers 404 again. The class still
  follows "when unsure": the slug is shared with referenced nodes and with the read-model port error, and no test holds
  the "never deleted" invariant. The cost is one extra transaction per retry of a mistyped id.
- **J3 `incomplete-operational-profile` is RETRYABLE.** The activation case reads the node's current profile, which a
  `PUT /operational-profile` completes; the profile-declaration case is a function of the request and the node's status.
  One slug, the state-dependent source decides.
- **J4 `serving-station-not-allowed` is RETRYABLE although two of its four sources are request-only.** The clean fix is
  its own problem type for "the serving station is not ACTIVE", a catalogue and spec change that widens this PR;
  recorded as a follow-up.
- **J5 `validation-failed` stays FINAL** although `checkProfileFits` reads the node's operator and `DeclareProfile`
  reads its type: both are fixed at registration.
- **J6 `lane-type-pair-not-allowed` is FINAL** although it reads two loaded nodes: node types never change. A missing
  node answers `not-found` first.
- **J7 `region-code-taken` is FINAL.** Briefing and code agree: no use case renames or deletes a region.
- **J8 `duplicate-lane` is RETRYABLE, against the first guess that it is FINAL.** The unique index is partial on OPEN
  lanes and `CloseLane` is a keyed route of this service.
- **J9 `unknown-region` is RETRYABLE** although ids are minted here: the 422 is the answer for a region id this database
  does not hold yet (a region defined later, seeded from another environment, or restored), the very shape of "a referenced
  fact not known yet".
- **J10 No problem type is OUTCOME**, see decision 6.

## Consequences

- A retry under the same key after any of the ten RETRYABLE refusals runs the request again instead of replaying a
  refusal; the success is then stored. A different body under that key is a new request. A client that sent a deliberately
  reused key after one of them gets its retry executed, not the old refusal. The OpenAPI text lists the ten.
- A released refusal costs one rolled-back transaction per retry, and the refused request writes no key row, so the key
  table no longer grows with them.
- Deterministic-key callers (vendor-connectivity: `POST /nodes` and `PUT /nodes/{nodeId}/operational-profile`, key = the
  submission id): this repository changes no caller. vendor-connectivity's `classify` (`submissions.go` 130-143) turns
  EVERY 4xx (`client.go` 84-85, `gateway.RefusalError`) into a terminal failed submission and retries only 5xx and
  outages, so it never retries a RETRYABLE refusal and gains nothing yet; a stored refusal never stuck it either. A
  follow-up in that repository (not made here) would treat the ten slugs above as "pending" and retry them up to
  `SUBMISSION_MAX_ATTEMPTS`.
- A new problem type is classified by the PR that adds it: the unit test is red otherwise, and the table above, the
  OpenAPI RETRYABLE sentence and the catalogue move together.
- Known limit, read from the code and not changed here: a unique violation inside the request transaction (the
  `INSERT` paths of `Nodes.Save`, `Lanes.Save` and `Regions.Save`: `vendor-node-code-taken`, `duplicate-lane`,
  `region-code-taken`) aborts the savepoint, so `transact`'s retry of an `ErrConcurrentUpdate` on the same unit would run
  on an aborted transaction; the typed refusals themselves are answered correctly. Left to a follow-up.
