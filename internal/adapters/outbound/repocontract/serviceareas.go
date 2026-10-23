package repocontract

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

var serviceAreaCases = []named{
	{"save then get round-trips the snapshot", areaRoundTrip},
	{"get of an unassigned prefix is ErrNotFound and matching is exact", areaGetMiss},
	{"a reassignment is stored under the next version", areaReassign},
	{"stale saves are ErrConcurrentUpdate", areaConcurrency},
}

// areaStation seeds a region and an ACTIVE delivery station.
func areaStation(t *testing.T, f Fixture) (*region.Region, *node.Node) {
	t.Helper()
	reg := seedRegion(t, f, "region-1", "SP")
	return reg, saveActive(t, f, reg, node.DeliveryStation, "node-s", "")
}

func areaRoundTrip(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	a := NewServiceArea(t, "013", st, reg, 1)
	noErr(t, f.ServiceAreas.Save(ctx, a))
	got, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	if !reflect.DeepEqual(got.Snapshot(), a.Snapshot()) {
		t.Fatalf("snapshot = %+v, want %+v", got.Snapshot(), a.Snapshot())
	}
}

func areaGetMiss(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	noErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)))
	for _, prefix := range []string{"01", "0130", "ABC", ""} {
		_, err := f.ServiceAreas.Get(ctx, servicePrefix(prefix))
		wantErr(t, err, ports.ErrNotFound)
	}
}

func areaReassign(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	noErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)))
	a, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	_, err = a.Reassign(5, st, reg, Now)
	noErr(t, err)
	noErr(t, f.ServiceAreas.Save(ctx, a))
	got, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	if got.AssignmentVersion() != 5 || got.Version() != 2 {
		t.Fatalf("assignment=%d version=%d, want 5 and 2", got.AssignmentVersion(), got.Version())
	}
}

func areaConcurrency(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	noErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)))
	winner, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	loser, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	_, err = winner.Reassign(2, st, reg, Now)
	noErr(t, err)
	_, err = loser.Reassign(3, st, reg, Now)
	noErr(t, err)
	noErr(t, f.ServiceAreas.Save(ctx, winner))
	wantErr(t, f.ServiceAreas.Save(ctx, loser), ports.ErrConcurrentUpdate)
	wantErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)), ports.ErrConcurrentUpdate)
}

// servicePrefix converts a literal to the prefix type.
func servicePrefix(s string) servicearea.PostalPrefix { return servicearea.PostalPrefix(s) }
