package usecases_test

import (
	"context"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

func openCommand() usecases.OpenLaneCommand {
	return usecases.OpenLaneCommand{
		OriginNodeID: "n-o", DestinationNodeID: "n-d", Mode: lane.ModeGround, NominalTransitMinutes: 120,
	}
}

func TestOpenLaneOpensALaneBetweenTwoActiveNodes(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	before := e.before()

	got, err := usecases.NewOpenLane(e.deps).Execute(context.Background(), openCommand())
	wantErr(t, err, nil)

	if got.LaneID != "lane-1" || got.Status != "OPEN" || got.Mode != "GROUND" || got.NominalTransitMinutes != 120 ||
		got.OriginNodeID != "n-o" || got.DestinationNodeID != "n-d" || got.ClosedAt != nil {
		t.Fatalf("view = %+v", got)
	}
	events := e.assertRaised(before, "LaneOpened")
	if events[0].SubjectID() != "lane-1" || events[0].AggregateVersion() != 1 {
		t.Fatalf("event = %+v", events[0])
	}
	if after := e.store.Counts(); after.Lanes != before.Lanes+1 {
		t.Fatalf("lanes = %d, want one new", after.Lanes)
	}
}

func TestOpenLaneAfterTheOldOneWasClosed(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	e.seedLane("lane-old", "n-o", "n-d", lane.ModeGround)
	if _, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), "lane-old"); err != nil {
		t.Fatal(err)
	}
	before := e.before()

	got, err := usecases.NewOpenLane(e.deps).Execute(context.Background(), openCommand())
	wantErr(t, err, nil)

	if got.Status != "OPEN" {
		t.Fatalf("view = %+v", got)
	}
	e.assertRaised(before, "LaneOpened")
}

func TestOpenLaneFailures(t *testing.T) {
	open := func(mutate func(*usecases.OpenLaneCommand)) func(*env) error {
		return func(e *env) error {
			cmd := openCommand()
			mutate(&cmd)
			_, err := usecases.NewOpenLane(e.deps).Execute(context.Background(), cmd)
			return err
		}
	}
	same := func(*usecases.OpenLaneCommand) {}
	ends := func(e *env) { e.seedLaneEnds() }
	with := func(set func(e *env)) func(e *env) { return func(e *env) { e.seedLaneEnds(); set(e) } }
	reverse := func(c *usecases.OpenLaneCommand) {
		c.OriginNodeID, c.DestinationNodeID = c.DestinationNodeID, c.OriginNodeID
	}

	runFailCases(t, []failCase{
		{"unknown origin", ends, open(func(c *usecases.OpenLaneCommand) { c.OriginNodeID = "ghost" }), ports.ErrNotFound},
		{"unknown destination", ends, open(func(c *usecases.OpenLaneCommand) { c.DestinationNodeID = "ghost" }), ports.ErrNotFound},
		{"loop", ends, open(func(c *usecases.OpenLaneCommand) { c.DestinationNodeID = c.OriginNodeID }), lane.ErrLaneLoop},
		{"type pair not allowed", ends, open(reverse), lane.ErrLaneTypePairNotAllowed},
		{"origin not active", func(e *env) {
			reg := e.seedRegion("r-1", "SP")
			ot, dt := laneTypes(t)
			e.seedNode(reg, ot, "n-o", "O", node.StatusProposed)
			e.seedNode(reg, dt, "n-d", "D", node.StatusActive)
		}, open(same), lane.ErrLaneEndpointNotActive},
		{"destination suspended", func(e *env) {
			reg := e.seedRegion("r-1", "SP")
			ot, dt := laneTypes(t)
			e.seedNode(reg, ot, "n-o", "O", node.StatusActive)
			e.seedNode(reg, dt, "n-d", "D", node.StatusSuspended)
		}, open(same), lane.ErrLaneEndpointNotActive},
		{"invalid mode", ends, open(func(c *usecases.OpenLaneCommand) { c.Mode = "SEA" }), shared.ErrValidation},
		{"invalid transit", ends, open(func(c *usecases.OpenLaneCommand) { c.NominalTransitMinutes = 0 }), shared.ErrValidation},
		{"duplicate open lane", with(func(e *env) { e.seedLane("lane-old", "n-o", "n-d", lane.ModeGround) }), open(same), lane.ErrDuplicateLane},
		{"repository raises the duplicate under a race", with(func(e *env) { e.lanes.saveErrs = []error{lane.ErrDuplicateLane} }), open(same), lane.ErrDuplicateLane},
		{"origin lookup fails", with(func(e *env) { e.nodes.getErr = errBoom }), open(same), errBoom},
		{"open lane lookup fails", with(func(e *env) { e.lanes.findErr = errBoom }), open(same), errBoom},
		{"save fails", with(func(e *env) { e.lanes.saveErrs = []error{errBoom} }), open(same), errBoom},
		{"outbox fails after the save", with(func(e *env) { e.outbox.errs = []error{errBoom} }), open(same), errBoom},
		{"conflict twice", with(func(e *env) {
			e.lanes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}), open(same), ports.ErrConcurrentUpdate},
	})
}

func TestOpenLaneRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	e.lanes.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	got, err := usecases.NewOpenLane(e.deps).Execute(context.Background(), openCommand())
	wantErr(t, err, nil)

	if got.Status != "OPEN" || e.lanes.saves != 2 {
		t.Fatalf("view=%+v saves=%d", got, e.lanes.saves)
	}
	e.assertRaised(before, "LaneOpened")
}

func TestCloseLaneClosesAnOpenLane(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	e.seedLane("lane-1", "n-o", "n-d", lane.ModeAir)
	before := e.before()

	got, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), "lane-1")
	wantErr(t, err, nil)

	if got.Status != "CLOSED" || got.ClosedAt == nil || !got.ClosedAt.Equal(repocontract.Now) {
		t.Fatalf("view = %+v", got)
	}
	events := e.assertRaised(before, "LaneClosed")
	if events[0].AggregateVersion() != 2 {
		t.Fatalf("event version = %d, want 2", events[0].AggregateVersion())
	}
	stored, err := e.store.Lanes().Get(context.Background(), "lane-1")
	wantErr(t, err, nil)
	if stored.Status() != lane.StatusClosed || stored.Version() != 2 {
		t.Fatalf("stored = %s v%d", stored.Status(), stored.Version())
	}
}

func TestCloseLaneFailures(t *testing.T) {
	closeLane := func(id lane.ID) func(*env) error {
		return func(e *env) error {
			_, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), id)
			return err
		}
	}
	open := func(set func(e *env)) func(e *env) {
		return func(e *env) { e.seedLaneEnds(); e.seedLane("lane-1", "n-o", "n-d", lane.ModeGround); set(e) }
	}
	runFailCases(t, []failCase{
		{"unknown lane", noSetup, closeLane("ghost"), ports.ErrNotFound},
		{"already closed", func(e *env) {
			open(noSetup)(e)
			if _, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), "lane-1"); err != nil {
				t.Fatal(err)
			}
		}, closeLane("lane-1"), shared.ErrIllegalTransition},
		{"load fails", func(e *env) { e.lanes.getErr = errBoom }, closeLane("lane-1"), errBoom},
		{"save fails", open(func(e *env) { e.lanes.saveErrs = []error{errBoom} }), closeLane("lane-1"), errBoom},
		{"outbox fails after the save", open(func(e *env) { e.outbox.errs = []error{errBoom} }), closeLane("lane-1"), errBoom},
		{"conflict twice", open(func(e *env) {
			e.lanes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}), closeLane("lane-1"), ports.ErrConcurrentUpdate},
	})
}

func TestCloseLaneRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	e.seedLane("lane-1", "n-o", "n-d", lane.ModeGround)
	e.lanes.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	got, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), "lane-1")
	wantErr(t, err, nil)

	if got.Status != "CLOSED" || e.lanes.saves != 2 {
		t.Fatalf("view=%+v saves=%d", got, e.lanes.saves)
	}
	e.assertRaised(before, "LaneClosed")
}
