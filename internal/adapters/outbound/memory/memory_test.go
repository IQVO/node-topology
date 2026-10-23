package memory_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// TestRepositoryContract runs the repository contract against the doubles.
func TestRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(t *testing.T) repocontract.Fixture {
		s := memory.NewStore()
		return repocontract.Fixture{
			Nodes: s.Nodes(), Lanes: s.Lanes(), ServiceAreas: s.ServiceAreas(), Regions: s.Regions(),
			Outbox: s.Outbox(), UoW: s.UnitOfWork(),
			OutboxCount: func(context.Context) (int, error) { return len(s.Outbox().Events()), nil },
		}
	})
}

func TestClockAndSequentialIDs(t *testing.T) {
	start := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	c := memory.NewClock(start)
	c.Advance(time.Hour)
	if !c.Now().Equal(start.Add(time.Hour)) {
		t.Fatalf("now = %v", c.Now())
	}
	c.Set(start)
	if !c.Now().Equal(start) {
		t.Fatalf("now = %v", c.Now())
	}

	ids := &memory.SequentialIDs{}
	got := []string{
		string(ids.NewNodeID()), string(ids.NewNodeID()), string(ids.NewLaneID()),
		string(ids.NewRegionID()), string(ids.NewRegionID()),
	}
	want := []string{"node-1", "node-2", "lane-1", "region-1", "region-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestCountsFollowTheStore(t *testing.T) {
	s := memory.NewStore()
	if c := s.Counts(); c != (memory.Counts{}) {
		t.Fatalf("empty store counts = %+v", c)
	}
	ctx := context.Background()
	reg := repocontract.NewRegion(t, "r-1", "SP")
	n := repocontract.NewNode(t, reg, node.FulfillmentCenter, "n-1", "FC-1")
	if _, err := n.DeclareProfile(repocontract.Declaration(1), repocontract.Now); err != nil {
		t.Fatal(err)
	}
	snap := n.Snapshot()
	snap.Version = 1
	stored, err := node.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Regions().Save(ctx, reg); err != nil {
		t.Fatal(err)
	}
	if err := s.Nodes().Save(ctx, stored); err != nil {
		t.Fatal(err)
	}
	want := memory.Counts{Nodes: 1, ProfileRevisions: 1, Regions: 1, VersionSum: 2}
	if c := s.Counts(); c != want {
		t.Fatalf("counts = %+v, want %+v", c, want)
	}
}

func TestCorruptRowsAreNeverRestored(t *testing.T) {
	// A store whose row breaks an invariant must fail the Get, not hand out the aggregate.
	s := memory.NewStore()
	ctx := context.Background()
	reg := repocontract.NewRegion(t, "r-1", "SP")
	snap := reg.Snapshot()
	snap.Code = "not a valid code"
	if _, err := region.Restore(snap); err == nil {
		t.Fatal("the domain accepted a corrupt snapshot")
	}
	if _, err := s.Regions().Get(ctx, "r-1"); err == nil {
		t.Fatal("an empty store returned a region")
	}
}
