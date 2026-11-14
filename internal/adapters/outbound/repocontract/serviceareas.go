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
	{saveGetRoundTrip, areaRoundTrip},
	{"get of an unassigned prefix is ErrNotFound and matching is exact", areaGetMiss},
	{"a reassignment is stored under the next version", areaReassign},
	{"stale saves are ErrConcurrentUpdate", areaConcurrency},
	{"a carrier-handoff area round-trips its mode and options", areaCarrierRoundTrip},
}

// areaStation seeds a region and an ACTIVE delivery station.
func areaStation(t *testing.T, f Fixture) (*region.Region, *node.Node) {
	t.Helper()
	reg := seedRegion(t, f, regionOne, "SP")
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
	_, err = a.Reassign(5, st, reg, servicearea.CarrierDelivery{}, Now)
	noErr(t, err)
	noErr(t, f.ServiceAreas.Save(ctx, a))
	got, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	if got.AssignmentVersion() != 5 || got.Version() != 2 {
		t.Fatalf("assignment=%d version=%d, want 5 and 2", got.AssignmentVersion(), got.Version())
	}
}

// areaConcurrency verifies the optimistic-concurrency rule of the area.
func areaConcurrency(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	noErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)))
	winner, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	loser, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	_, err = winner.Reassign(2, st, reg, servicearea.CarrierDelivery{}, Now)
	noErr(t, err)
	_, err = loser.Reassign(3, st, reg, servicearea.CarrierDelivery{}, Now)
	noErr(t, err)
	noErr(t, f.ServiceAreas.Save(ctx, winner))
	wantErr(t, f.ServiceAreas.Save(ctx, loser), ports.ErrConcurrentUpdate)
	wantErr(t, f.ServiceAreas.Save(ctx, NewServiceArea(t, "013", st, reg, 1)), ports.ErrConcurrentUpdate)
}

// areaCarrierRoundTrip seeds a carrier area with two options over an ACTIVE
// sortation centre and a second delivery station, saves it and proves both
// stores keep the mode and the whole option list (ADR 0016).
func areaCarrierRoundTrip(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg, st := areaStation(t, f)
	sc := saveActive(t, f, reg, node.SortationCenter, nodeC, "")
	a, _, err := servicearea.Assign("013", 1, st, reg, servicearea.CarrierDelivery{
		Mode: servicearea.ModeCarrierHandoff,
		Carriers: []servicearea.CarrierOption{
			{CarrierID: "carrier-a", HandoffNodeID: nodeC, CostPerPackageCents: 320, TransitMinutes: 95},
			{CarrierID: "carrier-b", HandoffNodeID: "node-s", CostPerPackageCents: 400, TransitMinutes: 60},
		},
		Handoffs: []*node.Node{sc, st},
	}, Now)
	if err != nil {
		t.Fatalf("assign carrier area: %v", err)
	}
	noErr(t, f.ServiceAreas.Save(ctx, a))
	got, err := f.ServiceAreas.Get(ctx, "013")
	noErr(t, err)
	if got.DeliveryMode() != servicearea.ModeCarrierHandoff || len(got.Carriers()) != 2 {
		t.Fatalf("mode=%s carriers=%d, want CARRIER_HANDOFF with 2 options", got.DeliveryMode(), len(got.Carriers()))
	}
	if !reflect.DeepEqual(got.Carriers(), a.Carriers()) {
		t.Fatalf("carriers = %+v, want %+v", got.Carriers(), a.Carriers())
	}
}

// servicePrefix converts a literal to the prefix type.
func servicePrefix(s string) servicearea.PostalPrefix { return servicearea.PostalPrefix(s) }
