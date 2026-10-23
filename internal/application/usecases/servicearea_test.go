package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

func assignCommand(version int) usecases.AssignServiceAreaCommand {
	return usecases.AssignServiceAreaCommand{PostalPrefix: "013", DeliveryStationNodeID: "n-s", AssignmentVersion: version}
}

// seedStation stores region r-1 and an ACTIVE delivery station n-s in it.
func seedStation(e *env) *region.Region {
	reg := e.seedRegion("r-1", "SP")
	e.seedNode(reg, node.DeliveryStation, "n-s", "", node.StatusActive)
	return reg
}

func TestAssignServiceAreaCreatesThenReassigns(t *testing.T) {
	e := newEnv(t)
	seedStation(e)
	uc := usecases.NewAssignServiceArea(e.deps)

	before := e.before()
	first, err := uc.Execute(context.Background(), assignCommand(1))
	wantErr(t, err, nil)
	if first.PostalPrefix != "013" || first.DeliveryStationNodeID != "n-s" || first.RegionID != "r-1" || first.AssignmentVersion != 1 {
		t.Fatalf("view = %+v", first)
	}
	events := e.assertRaised(before, "ServiceAreaAssigned")
	if events[0].SubjectID() != "013" || events[0].AggregateVersion() != 1 {
		t.Fatalf("event = %+v", events[0])
	}

	before = e.before()
	second, err := uc.Execute(context.Background(), assignCommand(7))
	wantErr(t, err, nil)
	if second.AssignmentVersion != 7 {
		t.Fatalf("view = %+v", second)
	}
	events = e.assertRaised(before, "ServiceAreaAssigned")
	if events[0].AggregateVersion() != 2 {
		t.Fatalf("event version = %d, want 2", events[0].AggregateVersion())
	}
	if after := e.store.Counts(); after.ServiceAreas != 1 {
		t.Fatalf("service areas = %d, a reassignment must not add a row", after.ServiceAreas)
	}
}

func TestAssignServiceAreaTakesTheRegionFromTheStation(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-1", "SP")
	other := e.seedRegion("r-2", "RJ")
	e.seedNode(other, node.DeliveryStation, "n-s", "", node.StatusActive)

	got, err := usecases.NewAssignServiceArea(e.deps).Execute(context.Background(), assignCommand(1))
	wantErr(t, err, nil)
	if got.RegionID != "r-2" {
		t.Fatalf("regionId = %s, want r-2 (the station's)", got.RegionID)
	}
}

func TestAssignServiceAreaFailures(t *testing.T) {
	assign := func(mutate func(*usecases.AssignServiceAreaCommand)) func(*env) error {
		return func(e *env) error {
			cmd := assignCommand(1)
			mutate(&cmd)
			_, err := usecases.NewAssignServiceArea(e.deps).Execute(context.Background(), cmd)
			return err
		}
	}
	same := func(*usecases.AssignServiceAreaCommand) {}
	station := func(e *env) { seedStation(e) }
	with := func(set func(e *env)) func(e *env) { return func(e *env) { seedStation(e); set(e) } }
	assigned := func(e *env) { seedStation(e); e.seedArea("013", "n-s", mustRegion(e), 3) }
	conflict := []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}

	runFailCases(t, []failCase{
		{"unknown station", noSetup, assign(same), ports.ErrNotFound},
		{"not a delivery station", func(e *env) {
			e.seedNode(e.seedRegion("r-1", "SP"), node.FulfillmentCenter, "n-s", "FC-1", node.StatusActive)
		}, assign(same), servicearea.ErrNotADeliveryStation},
		{"station not active", func(e *env) {
			e.seedNode(e.seedRegion("r-1", "SP"), node.DeliveryStation, "n-s", "", node.StatusSuspended)
		}, assign(same), servicearea.ErrNotADeliveryStation},
		{"station region is not defined", func(e *env) {
			ghost := repocontract.NewRegion(t, "r-ghost", "GH") // never stored
			e.seedNode(ghost, node.DeliveryStation, "n-s", "", node.StatusActive)
		}, assign(same), region.ErrUnknownRegion},
		{"invalid prefix", station, assign(func(c *usecases.AssignServiceAreaCommand) { c.PostalPrefix = "01 3" }), shared.ErrValidation},
		{"invalid version", station, assign(func(c *usecases.AssignServiceAreaCommand) { c.AssignmentVersion = 0 }), shared.ErrValidation},
		{"stale reassignment", assigned, assign(func(c *usecases.AssignServiceAreaCommand) { c.AssignmentVersion = 3 }), servicearea.ErrStaleAssignmentVersion},
		{"reassignment to a station that is not one", func(e *env) {
			assigned(e)
			e.seedNode(mustRegion(e), node.FulfillmentCenter, "n-fc", "FC-1", node.StatusActive)
		}, assign(func(c *usecases.AssignServiceAreaCommand) { c.AssignmentVersion = 4; c.DeliveryStationNodeID = "n-fc" }), servicearea.ErrNotADeliveryStation},
		{"station lookup fails", func(e *env) { e.nodes.getErr = errBoom }, assign(same), errBoom},
		{"region lookup fails", with(func(e *env) { e.regions.getErr = errBoom }), assign(same), errBoom},
		{"service area lookup fails", with(func(e *env) { e.areas.getErr = errBoom }), assign(same), errBoom},
		{"save fails", with(func(e *env) { e.areas.saveErrs = []error{errBoom} }), assign(same), errBoom},
		{"outbox fails after the save", with(func(e *env) { e.outbox.errs = []error{errBoom} }), assign(same), errBoom},
		{"conflict twice", with(func(e *env) { e.areas.saveErrs = conflict }), assign(same), ports.ErrConcurrentUpdate},
	})
}

func TestAssignServiceAreaRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	seedStation(e)
	e.areas.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	got, err := usecases.NewAssignServiceArea(e.deps).Execute(context.Background(), assignCommand(1))
	wantErr(t, err, nil)

	if got.AssignmentVersion != 1 || e.areas.saves != 2 {
		t.Fatalf("view=%+v saves=%d", got, e.areas.saves)
	}
	e.assertRaised(before, "ServiceAreaAssigned")
}
