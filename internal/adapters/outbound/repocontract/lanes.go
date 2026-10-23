package repocontract

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
)

var laneCases = []named{
	{"save then get round-trips the snapshot", laneRoundTrip},
	{"get of an unknown id is ErrNotFound", laneGetMiss},
	{"find open hits only the OPEN lane of the triple", laneFindOpen},
	{"a second OPEN lane for the triple is ErrDuplicateLane", laneDuplicate},
	{"a closed lane frees the triple and keeps its history", laneReopenAfterClose},
	{"stale saves are ErrConcurrentUpdate", laneConcurrency},
	{"list filters by origin and destination, open and closed, ordered by id", laneList},
}

// laneEnds seeds a region and two ACTIVE nodes forming an allowed pair.
func laneEnds(t *testing.T, f Fixture) (origin, destination *node.Node) {
	t.Helper()
	reg := seedRegion(t, f, "region-1", "SP")
	ot, dt := AllowedPair(t)
	return saveActive(t, f, reg, ot, "node-o", "FC-O"), saveActive(t, f, reg, dt, "node-d", "FC-D")
}

func laneRoundTrip(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	l := NewLane(t, "lane-1", o, d, lane.ModeGround)
	noErr(t, f.Lanes.Save(ctx, l))
	got, err := f.Lanes.Get(ctx, "lane-1")
	noErr(t, err)
	if !reflect.DeepEqual(got.Snapshot(), l.Snapshot()) {
		t.Fatalf("snapshot = %+v, want %+v", got.Snapshot(), l.Snapshot())
	}
}

func laneGetMiss(t *testing.T, f Fixture) {
	_, err := f.Lanes.Get(context.Background(), "nope")
	wantErr(t, err, ports.ErrNotFound)
}

func laneFindOpen(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-1", o, d, lane.ModeGround)))
	got, err := f.Lanes.FindOpen(ctx, "node-o", "node-d", lane.ModeGround)
	noErr(t, err)
	if got.ID() != "lane-1" {
		t.Fatalf("id = %s, want lane-1", got.ID())
	}
	for _, miss := range []struct {
		o, d node.ID
		m    lane.Mode
	}{{"node-o", "node-d", lane.ModeAir}, {"node-d", "node-o", lane.ModeGround}, {"node-o", "node-x", lane.ModeGround}} {
		_, err = f.Lanes.FindOpen(ctx, miss.o, miss.d, miss.m)
		wantErr(t, err, ports.ErrNotFound)
	}
}

func laneDuplicate(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-1", o, d, lane.ModeGround)))
	wantErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-2", o, d, lane.ModeGround)), lane.ErrDuplicateLane)
	_, err := f.Lanes.Get(ctx, "lane-2")
	wantErr(t, err, ports.ErrNotFound)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-3", o, d, lane.ModeAir)))
}

func laneReopenAfterClose(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-1", o, d, lane.ModeGround)))
	first, err := f.Lanes.Get(ctx, "lane-1")
	noErr(t, err)
	_, err = first.Close(Now)
	noErr(t, err)
	noErr(t, f.Lanes.Save(ctx, first))
	_, err = f.Lanes.FindOpen(ctx, "node-o", "node-d", lane.ModeGround)
	wantErr(t, err, ports.ErrNotFound)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-2", o, d, lane.ModeGround)))
	closed, err := f.Lanes.Get(ctx, "lane-1")
	noErr(t, err)
	if closed.Status() != lane.StatusClosed || closed.ClosedAt().IsZero() {
		t.Fatalf("lane-1 = %s closedAt=%v, want CLOSED with a closing time", closed.Status(), closed.ClosedAt())
	}
}

func laneConcurrency(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-1", o, d, lane.ModeGround)))
	winner, err := f.Lanes.Get(ctx, "lane-1")
	noErr(t, err)
	loser, err := f.Lanes.Get(ctx, "lane-1")
	noErr(t, err)
	_, err = winner.Close(Now)
	noErr(t, err)
	_, err = loser.Close(Now)
	noErr(t, err)
	noErr(t, f.Lanes.Save(ctx, winner))
	wantErr(t, f.Lanes.Save(ctx, loser), ports.ErrConcurrentUpdate)
	wantErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-1", o, d, lane.ModeAir)), ports.ErrConcurrentUpdate)
}

func laneList(t *testing.T, f Fixture) {
	ctx := context.Background()
	o, d := laneEnds(t, f)
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-b", o, d, lane.ModeAir)))
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-a", o, d, lane.ModeGround)))
	// lane-c is saved open, then reloaded and closed.
	ot, _ := AllowedPair(t)
	other := saveActive(t, f, seedRegion(t, f, "region-2", "RJ"), ot, "node-o2", "FC-O2")
	noErr(t, f.Lanes.Save(ctx, NewLane(t, "lane-c", other, d, lane.ModeGround)))
	reloaded, err := f.Lanes.Get(ctx, "lane-c")
	noErr(t, err)
	_, err = reloaded.Close(Now)
	noErr(t, err)
	noErr(t, f.Lanes.Save(ctx, reloaded))

	cases := []struct {
		name   string
		filter repository.LaneFilter
		want   []lane.ID
	}{
		{"no filter", repository.LaneFilter{}, []lane.ID{"lane-a", "lane-b", "lane-c"}},
		{"origin", repository.LaneFilter{Origin: "node-o"}, []lane.ID{"lane-a", "lane-b"}},
		{"destination", repository.LaneFilter{Destination: "node-d"}, []lane.ID{"lane-a", "lane-b", "lane-c"}},
		{"both", repository.LaneFilter{Origin: "node-o2", Destination: "node-d"}, []lane.ID{"lane-c"}},
		{"no match", repository.LaneFilter{Origin: "node-x"}, []lane.ID{}},
	}
	for _, c := range cases {
		got, err := f.Lanes.List(ctx, c.filter)
		noErr(t, err)
		ids := make([]lane.ID, len(got))
		for i, l := range got {
			ids[i] = l.ID()
		}
		if !reflect.DeepEqual(ids, c.want) {
			t.Fatalf("%s: ids = %v, want %v", c.name, ids, c.want)
		}
	}
}
