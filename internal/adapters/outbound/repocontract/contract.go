// Package repocontract is the behavioural contract of the repository ports:
// one function, Run, that takes a Factory of a fresh empty fixture and checks
// every promise of internal/application/ports. The in-memory doubles pass it in
// their own tests and the Postgres adapters pass it in step 3, so the two can
// never drift. It is test support: cmd/** must never import it.
package repocontract

import (
	"context"
	"errors"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// Fixture is one implementation of every port, over ONE empty database.
type Fixture struct {
	Nodes        ports.NodeRepository
	Lanes        ports.LaneRepository
	ServiceAreas ports.ServiceAreaRepository
	Regions      ports.RegionRepository
	Outbox       ports.EventOutbox
	UoW          ports.UnitOfWork
	// OutboxCount returns how many events are stored in the outbox. Nil skips
	// the outbox atomicity checks.
	OutboxCount func(ctx context.Context) (int, error)
}

// Factory returns a fresh, empty Fixture and registers its own cleanup with t.
type Factory func(t *testing.T) Fixture

// named is one contract case.
type named struct {
	name string
	run  func(*testing.T, Fixture)
}

// Run executes the whole contract; every case gets a fresh Fixture.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	groups := []struct {
		name  string
		cases []named
	}{
		{"Region", regionCases},
		{"Node", nodeCases},
		{"Lane", laneCases},
		{"ServiceArea", serviceAreaCases},
		{"UnitOfWork", unitOfWorkCases},
	}
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			for _, c := range g.cases {
				t.Run(c.name, func(t *testing.T) { c.run(t, factory(t)) })
			}
		})
	}
}

// wantErr fails unless err matches target.
func wantErr(t testing.TB, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

// noErr fails on err.
func noErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// SeedRegion stores a region through f and returns it. Adapter-specific tests
// reuse it to build the rows the contract builds.
func SeedRegion(t *testing.T, f Fixture, id region.ID, code string) *region.Region {
	t.Helper()
	return seedRegion(t, f, id, code)
}

// SaveActive stores an ACTIVE node through f the way the service does, one
// saved version per command, and returns it as stored.
func SaveActive(t *testing.T, f Fixture, reg *region.Region, typ node.Type, id node.ID, code string) *node.Node {
	t.Helper()
	return saveActive(t, f, reg, typ, id, code)
}
