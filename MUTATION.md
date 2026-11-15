# Mutation testing: residual mutants

Measured with gremlins v0.6.0, one worker, one `gremlins unleash ./internal/domain/<pkg>` per package
(`make mutation`, the same loop as the `mutation-fast` CI job). Efficacy is 100.00% in every package.

## NOT COVERED by the cover profile, killed in fact (not equivalent)

| Package | File:line | Mutators | Why it is reported NOT COVERED |
| --- | --- | --- | --- |
| servicearea | `carriers.go:99` (`o.CostPerPackageCents < 0 \|\| o.CostPerPackageCents > MaxCarrierCostCents`) | CONDITIONALS_BOUNDARY, CONDITIONALS_NEGATION on both operators (4) | `case` expression of a tagless `switch` |
| servicearea | `carriers.go:102` (`o.TransitMinutes < 1 \|\| o.TransitMinutes > shared.MaxInt32`) | CONDITIONALS_BOUNDARY, CONDITIONALS_NEGATION on both operators (4) | same |

Go's cover tool instruments the body of each `case` clause and never the `case` expression itself, so
gremlins sees no covered block on those two lines (HARNESS.md "tagless switch" pitfall). The tests
execute and assert both lines: `TestCarrierOptionListShape`, `TestRestoreRejectsCorruptCarrierRows`
and `TestCarrierOptionAcceptsTheLowerBounds` fail under each of the 8 mutations. Verified by applying
each mutation by hand and running `go test ./internal/domain/servicearea` (all 8 fail).

Their cost is on the mutator-coverage figure only: servicearea measures 87.30%, so
`.gremlins.yaml` carries `mutant-coverage: 87`. Rewriting that `switch` as an `if` chain (production,
behaviour-preserving) moves both lines into covered blocks and lets the gate rise to 99.

## Equivalent mutants

None.
