//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

const racers = 8

// race runs fn(i) for i in [0, racers) at the same instant and returns the
// errors by index.
func race(fn func(i int) error) []error {
	errs := make([]error, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// assertOneWinner fails unless exactly one racer succeeded and every other one
// failed with loser.
func assertOneWinner(t *testing.T, errs []error, loser error) {
	t.Helper()
	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case !errors.Is(err, loser):
			t.Fatalf("racer %d: error = %v, want %v", i, err, loser)
		}
	}
	if winners != 1 {
		t.Fatalf("%d racers won, want exactly 1 (errors: %v)", winners, errs)
	}
}

func TestConcurrentSavesOfOneNodeVersionHaveOneWinner(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, repocontract.NewNode(t, reg, node.DeliveryStation, "node-1", "")))

	loaded := make([]*node.Node, racers)
	for i := range loaded {
		n, err := f.Nodes.Get(ctx, "node-1")
		noErr(t, err)
		_, err = n.Activate(repocontract.Now)
		noErr(t, err)
		loaded[i] = n
	}
	errs := race(func(i int) error { return f.Nodes.Save(ctx, loaded[i]) })

	assertOneWinner(t, errs, ports.ErrConcurrentUpdate)
	got, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if got.Version() != 2 || got.Status() != node.StatusActive {
		t.Fatalf("stored version=%d status=%s, want 2 ACTIVE", got.Version(), got.Status())
	}
}

func TestConcurrentCreatesOfOneIDHaveOneWinner(t *testing.T) {
	f, pool := newFixture(t)
	ctx := context.Background()
	regions := make([]*region.Region, racers)
	for i := range regions {
		regions[i] = repocontract.NewRegion(t, "region-1", "SP")
	}
	errs := race(func(i int) error { return f.Regions.Save(ctx, regions[i]) })

	// The id is the same, so the loser sees either the primary key or, when
	// the code index fires first, the taken code: both mean "not you".
	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case !errors.Is(err, ports.ErrConcurrentUpdate) && !errors.Is(err, region.ErrRegionCodeTaken):
			t.Fatalf("racer %d: unexpected error %v", i, err)
		}
	}
	if winners != 1 || count(t, pool, "regions") != 1 {
		t.Fatalf("winners = %d, want 1", winners)
	}
}

func TestConcurrentRegionsWithOneCodeHaveOneWinner(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	regions := make([]*region.Region, racers)
	for i := range regions {
		regions[i] = repocontract.NewRegion(t, region.ID("region-"+string(rune('a'+i))), "SP")
	}
	errs := race(func(i int) error { return f.Regions.Save(ctx, regions[i]) })
	assertOneWinner(t, errs, region.ErrRegionCodeTaken)
}

func TestConcurrentNodesWithOneVendorCodeHaveOneWinner(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	nodes := make([]*node.Node, racers)
	for i := range nodes {
		nodes[i] = repocontract.NewNode(t, reg, node.FulfillmentCenter, node.ID("node-"+string(rune('a'+i))), "FC-1")
	}
	errs := race(func(i int) error { return f.Nodes.Save(ctx, nodes[i]) })
	assertOneWinner(t, errs, node.ErrVendorNodeCodeTaken)
}

func TestConcurrentOpenLanesOfOneTripleHaveOneWinner(t *testing.T) {
	f, _ := newFixture(t)
	ctx := context.Background()
	reg := repocontract.SeedRegion(t, f, "region-1", "SP")
	ot, dt := repocontract.AllowedPair(t)
	origin := repocontract.SaveActive(t, f, reg, ot, "node-o", "FC-O")
	dest := repocontract.SaveActive(t, f, reg, dt, "node-d", "FC-D")
	lanes := make([]*lane.Lane, racers)
	for i := range lanes {
		lanes[i] = repocontract.NewLane(t, lane.ID("lane-"+string(rune('a'+i))), origin, dest, lane.ModeGround)
	}
	errs := race(func(i int) error { return f.Lanes.Save(ctx, lanes[i]) })
	assertOneWinner(t, errs, lane.ErrDuplicateLane)
}
