package usecases_test

import (
	"context"
	"errors"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// failCase is one command that must fail with want and leave the store as it
// was, with nothing in the outbox.
type failCase struct {
	name  string
	setup func(e *env)
	run   func(e *env) error
	want  error
}

func runFailCases(t *testing.T, cases []failCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			c.setup(e)
			before := e.before()
			err := c.run(e)
			if err == nil {
				t.Fatal("the command succeeded, want a failure")
			}
			wantErr(t, err, c.want)
			e.assertUnchanged(before)
			e.assertRaised(before)
		})
	}
}

func noSetup(*env) {}

func fcCommand() usecases.RegisterNodeCommand {
	return usecases.RegisterNodeCommand{
		Type: node.FulfillmentCenter, Subtype: node.SubtypeSortable, Operator: node.OperatorVendor,
		Vendor:   &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-1"},
		RegionID: "r-1", Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}
}

func stationCommand() usecases.RegisterNodeCommand {
	return usecases.RegisterNodeCommand{
		Type: node.DeliveryStation, Operator: node.OperatorNetwork, RegionID: "r-1",
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}
}

func TestRegisterNodeRegistersAVendorFulfillmentCenter(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-1", "SP")
	before := e.before()

	got, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), fcCommand())
	wantErr(t, err, nil)

	if got.NodeID != "node-1" || got.Status != "PROPOSED" || got.NodeType != "FULFILLMENT_CENTER" || got.Subtype != "SORTABLE" {
		t.Fatalf("view = %+v", got)
	}
	if got.VendorRef == nil || got.VendorRef.VendorNodeCode != "FC-1" || !got.Capabilities.Outbound || got.OperationalProfile != nil {
		t.Fatalf("view = %+v", got)
	}
	events := e.assertRaised(before, "NodeRegistered")
	if events[0].SubjectID() != "node-1" || events[0].AggregateVersion() != 1 || !events[0].OccurredAt().Equal(repocontract.Now) {
		t.Fatalf("event = %+v", events[0])
	}
	stored, err := e.store.Nodes().Get(context.Background(), "node-1")
	wantErr(t, err, nil)
	if stored.Status() != node.StatusProposed || stored.Version() != 1 {
		t.Fatalf("stored = %s v%d", stored.Status(), stored.Version())
	}
}

func TestRegisterNodeRegistersANetworkNodeWithoutVendor(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-1", "SP")
	before := e.before()

	got, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), stationCommand())
	wantErr(t, err, nil)

	if got.VendorRef != nil || got.Operator != "NETWORK" || !got.Capabilities.LastMile {
		t.Fatalf("view = %+v", got)
	}
	e.assertRaised(before, "NodeRegistered")
}

func TestRegisterNodeFailures(t *testing.T) {
	register := func(mutate func(*usecases.RegisterNodeCommand)) func(*env) error {
		return func(e *env) error {
			cmd := fcCommand()
			mutate(&cmd)
			_, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), cmd)
			return err
		}
	}
	withRegion := func(e *env) { e.seedRegion("r-1", "SP") }
	runFailCases(t, []failCase{
		{"unknown region", noSetup, register(func(*usecases.RegisterNodeCommand) {}), region.ErrUnknownRegion},
		{"operator not allowed for the type", withRegion, register(func(c *usecases.RegisterNodeCommand) {
			c.Type, c.Operator, c.Vendor, c.Subtype = node.DeliveryStation, node.OperatorVendor, nil, ""
		}), node.ErrOperatorNotAllowedForNodeType},
		{"subtype not allowed", withRegion, register(func(c *usecases.RegisterNodeCommand) {
			c.Type, c.Subtype = node.ReturnsCenter, node.SubtypeFresh
		}), node.ErrSubtypeNotAllowed},
		{"validation failure", withRegion, register(func(c *usecases.RegisterNodeCommand) { c.Timezone = "" }), shared.ErrValidation},
		{"vendor node code taken", func(e *env) {
			reg := e.seedRegion("r-1", "SP")
			e.seedNode(reg, node.FulfillmentCenter, "n-old", "FC-1", node.StatusProposed)
		}, register(func(*usecases.RegisterNodeCommand) {}), node.ErrVendorNodeCodeTaken},
		{"repository raises the taken code under a race", func(e *env) {
			withRegion(e)
			e.nodes.saveErrs = []error{node.ErrVendorNodeCodeTaken}
		}, register(func(*usecases.RegisterNodeCommand) {}), node.ErrVendorNodeCodeTaken},
		{"region lookup fails", func(e *env) { e.regions.getErr = errBoom }, register(func(*usecases.RegisterNodeCommand) {}), errBoom},
		{"vendor code lookup fails", func(e *env) { withRegion(e); e.nodes.findErr = errBoom }, register(func(*usecases.RegisterNodeCommand) {}), errBoom},
		{"save fails", func(e *env) { withRegion(e); e.nodes.saveErrs = []error{errBoom} }, register(func(*usecases.RegisterNodeCommand) {}), errBoom},
		{"outbox fails after the save", func(e *env) { withRegion(e); e.outbox.errs = []error{errBoom} }, register(func(*usecases.RegisterNodeCommand) {}), errBoom},
		{"conflict twice", func(e *env) {
			withRegion(e)
			e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}, register(func(*usecases.RegisterNodeCommand) {}), ports.ErrConcurrentUpdate},
	})
}

func TestRegisterNodeRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-1", "SP")
	e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	got, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), fcCommand())
	wantErr(t, err, nil)

	if e.nodes.saves != 2 {
		t.Fatalf("save attempts = %d, want 2", e.nodes.saves)
	}
	if got.NodeID != "node-2" {
		t.Fatalf("id = %s, want a fresh id from the retry (node-2)", got.NodeID)
	}
	e.assertRaised(before, "NodeRegistered")
	if after := e.store.Counts(); after.Nodes != before.Nodes+1 {
		t.Fatalf("nodes = %d, want exactly one new", after.Nodes)
	}
}

func TestDeclareOperationalProfileAppendsRevisions(t *testing.T) {
	e := newEnv(t)
	reg := e.seedRegion("r-1", "SP")
	e.seedNode(reg, node.FulfillmentCenter, "n-1", "FC-1", node.StatusProposed)
	uc := usecases.NewDeclareOperationalProfile(e.deps)

	for rev := 1; rev <= 2; rev++ {
		before := e.before()
		got, err := uc.Execute(context.Background(), usecases.DeclareOperationalProfileCommand{
			NodeID: "n-1", Declaration: repocontract.Declaration(rev),
		})
		wantErr(t, err, nil)
		assertProfile(t, got, rev)
		events := e.assertRaised(before, "NodeOperationalProfileDeclared")
		if events[0].AggregateVersion() != int64(rev+1) {
			t.Fatalf("event version = %d, want %d", events[0].AggregateVersion(), rev+1)
		}
		if after := e.store.Counts(); after.ProfileRevisions != before.ProfileRevisions+1 {
			t.Fatalf("profile rows = %d, want one more than %d", after.ProfileRevisions, before.ProfileRevisions)
		}
	}
	rows, err := e.store.Nodes().ProfileRevisions(context.Background(), "n-1")
	wantErr(t, err, nil)
	if len(rows) != 2 || rows[0].Revision != 1 || rows[1].Revision != 2 {
		t.Fatalf("revisions = %+v", rows)
	}
}

func assertProfile(t *testing.T, got views.Node, rev int) {
	t.Helper()
	p := got.OperationalProfile
	if p == nil || p.ProfileRevision != rev || p.HandlingTimeMinutes != 30 || p.DeclaredBy != "NETWORK_OPS" {
		t.Fatalf("operationalProfile = %+v, want revision %d", p, rev)
	}
	if got.Status != "PROPOSED" {
		t.Fatalf("status = %s, a profile declaration changes no state", got.Status)
	}
}

func TestDeclareOperationalProfileFailures(t *testing.T) {
	declare := func(id node.ID, mutate func(*node.ProfileDeclaration)) func(*env) error {
		return func(e *env) error {
			d := repocontract.Declaration(2)
			mutate(&d)
			_, err := usecases.NewDeclareOperationalProfile(e.deps).Execute(context.Background(),
				usecases.DeclareOperationalProfileCommand{NodeID: id, Declaration: d})
			return err
		}
	}
	keep := func(*node.ProfileDeclaration) {}
	seed := func(typ node.Type, status node.Status) func(*env) {
		return func(e *env) { e.seedNode(e.seedRegion("r-1", "SP"), typ, "n-1", "FC-1", status) }
	}
	runFailCases(t, []failCase{
		{"unknown node", noSetup, declare("n-1", keep), ports.ErrNotFound},
		{"stale revision", seed(node.FulfillmentCenter, node.StatusActive), declare("n-1", func(d *node.ProfileDeclaration) { d.Revision = 1 }), node.ErrStaleProfileRevision},
		{"incomplete profile on an active fulfillment centre", seed(node.FulfillmentCenter, node.StatusActive), declare("n-1", func(d *node.ProfileDeclaration) { d.HandlingTimeMinutes = 0 }), node.ErrIncompleteOperationalProfile},
		{"cutoff outside operating hours", seed(node.FulfillmentCenter, node.StatusProposed), declare("n-1", func(d *node.ProfileDeclaration) {
			d.Cutoffs = []node.CutoffSlot{{LocalTime: "23:00", Days: []node.Weekday{node.Monday}}}
		}), node.ErrCutoffOutsideOperatingHours},
		{"sort dwell on a node that does not sort", seed(node.FulfillmentCenter, node.StatusProposed), declare("n-1", func(d *node.ProfileDeclaration) { v := 5; d.SortDwellMinutes = &v }), node.ErrSortDwellNotAllowed},
		{"retired node", seed(node.FulfillmentCenter, node.StatusRetired), declare("n-1", keep), node.ErrNodeRetired},
		{"validation failure", seed(node.FulfillmentCenter, node.StatusProposed), declare("n-1", func(d *node.ProfileDeclaration) { d.Timezone = "" }), shared.ErrValidation},
		{"load fails", func(e *env) { e.nodes.getErr = errBoom }, declare("n-1", keep), errBoom},
		{"save fails", func(e *env) {
			seed(node.FulfillmentCenter, node.StatusProposed)(e)
			e.nodes.saveErrs = []error{errBoom}
		}, declare("n-1", keep), errBoom},
		{"outbox fails after the save", func(e *env) { seed(node.FulfillmentCenter, node.StatusProposed)(e); e.outbox.errs = []error{errBoom} }, declare("n-1", keep), errBoom},
		{"conflict twice", func(e *env) {
			seed(node.FulfillmentCenter, node.StatusProposed)(e)
			e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}, declare("n-1", keep), ports.ErrConcurrentUpdate},
	})
}

// transition is one lifecycle command.
type transition struct {
	name  string
	from  node.Status
	to    string
	event string
	run   func(e *env, id node.ID) (views.Node, error)
	typ   node.Type
}

func lifecycle() []transition {
	activate := func(e *env, id node.ID) (views.Node, error) {
		return usecases.NewActivateNode(e.deps).Execute(context.Background(), id)
	}
	suspend := func(e *env, id node.ID) (views.Node, error) {
		return usecases.NewSuspendNode(e.deps).Execute(context.Background(), id, "audit")
	}
	retire := func(e *env, id node.ID) (views.Node, error) {
		return usecases.NewRetireNode(e.deps).Execute(context.Background(), id)
	}
	return []transition{
		{name: "activate a proposed station", from: node.StatusProposed, to: "ACTIVE", event: "NodeActivated", run: activate, typ: node.DeliveryStation},
		{name: "activate a suspended fulfillment centre", from: node.StatusSuspended, to: "ACTIVE", event: "NodeActivated", run: activate, typ: node.FulfillmentCenter},
		{name: "suspend an active node", from: node.StatusActive, to: "SUSPENDED", event: "NodeSuspended", run: suspend, typ: node.DeliveryStation},
		{name: "retire an active node", from: node.StatusActive, to: "RETIRED", event: "NodeRetired", run: retire, typ: node.DeliveryStation},
		{name: "retire a suspended node", from: node.StatusSuspended, to: "RETIRED", event: "NodeRetired", run: retire, typ: node.DeliveryStation},
	}
}

func TestLifecycleCommandsMoveTheNodeAndRaiseOneEvent(t *testing.T) {
	for _, tr := range lifecycle() {
		t.Run(tr.name, func(t *testing.T) {
			e := newEnv(t)
			e.seedNode(e.seedRegion("r-1", "SP"), tr.typ, "n-1", "FC-1", tr.from)
			before := e.before()

			got, err := tr.run(e, "n-1")
			wantErr(t, err, nil)

			if got.Status != tr.to {
				t.Fatalf("status = %s, want %s", got.Status, tr.to)
			}
			events := e.assertRaised(before, tr.event)
			if events[0].AggregateVersion() != 2 {
				t.Fatalf("event version = %d, want 2", events[0].AggregateVersion())
			}
			stored, err := e.store.Nodes().Get(context.Background(), "n-1")
			wantErr(t, err, nil)
			if string(stored.Status()) != tr.to || stored.Version() != 2 {
				t.Fatalf("stored = %s v%d", stored.Status(), stored.Version())
			}
		})
	}
}

func TestSuspendViewCarriesTheReasonAndTime(t *testing.T) {
	e := newEnv(t)
	e.seedNode(e.seedRegion("r-1", "SP"), node.DeliveryStation, "n-1", "", node.StatusActive)
	got, err := usecases.NewSuspendNode(e.deps).Execute(context.Background(), "n-1", "flood")
	wantErr(t, err, nil)
	if got.SuspendReason != "flood" || got.SuspendedAt == nil || !got.SuspendedAt.Equal(repocontract.Now) || got.ActivatedAt == nil {
		t.Fatalf("view = %+v", got)
	}
}

func TestLifecycleCommandFailures(t *testing.T) {
	type command func(e *env, id node.ID) error
	activate := func(e *env, id node.ID) error {
		_, err := usecases.NewActivateNode(e.deps).Execute(context.Background(), id)
		return err
	}
	suspend := func(reason string) command {
		return func(e *env, id node.ID) error {
			_, err := usecases.NewSuspendNode(e.deps).Execute(context.Background(), id, reason)
			return err
		}
	}
	retire := func(e *env, id node.ID) error {
		_, err := usecases.NewRetireNode(e.deps).Execute(context.Background(), id)
		return err
	}
	seed := func(typ node.Type, status node.Status) func(*env) {
		return func(e *env) { e.seedNode(e.seedRegion("r-1", "SP"), typ, "n-1", "FC-1", status) }
	}
	on := func(c command) func(*env) error { return func(e *env) error { return c(e, "n-1") } }
	both := func(first func(*env), second func(*env)) func(*env) { return func(e *env) { first(e); second(e) } }
	conflict := func(e *env) { e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate} }

	runFailCases(t, []failCase{
		{"activate: unknown node", noSetup, on(activate), ports.ErrNotFound},
		{"activate: already active", seed(node.DeliveryStation, node.StatusActive), on(activate), shared.ErrIllegalTransition},
		{"activate: retired", seed(node.DeliveryStation, node.StatusRetired), on(activate), node.ErrNodeRetired},
		{"activate: fulfillment centre without a profile", seed(node.FulfillmentCenter, node.StatusProposed), on(activate), node.ErrIncompleteOperationalProfile},
		{"activate: load fails", func(e *env) { e.nodes.getErr = errBoom }, on(activate), errBoom},
		{"activate: save fails", both(seed(node.DeliveryStation, node.StatusProposed), func(e *env) { e.nodes.saveErrs = []error{errBoom} }), on(activate), errBoom},
		{"activate: outbox fails after the save", both(seed(node.DeliveryStation, node.StatusProposed), func(e *env) { e.outbox.errs = []error{errBoom} }), on(activate), errBoom},
		{"activate: conflict twice", both(seed(node.DeliveryStation, node.StatusProposed), conflict), on(activate), ports.ErrConcurrentUpdate},
		{"suspend: unknown node", noSetup, on(suspend("audit")), ports.ErrNotFound},
		{"suspend: proposed", seed(node.DeliveryStation, node.StatusProposed), on(suspend("audit")), shared.ErrIllegalTransition},
		{"suspend: retired", seed(node.DeliveryStation, node.StatusRetired), on(suspend("audit")), node.ErrNodeRetired},
		{"suspend: blank reason", seed(node.DeliveryStation, node.StatusActive), on(suspend("  ")), shared.ErrValidation},
		{"suspend: conflict twice", both(seed(node.DeliveryStation, node.StatusActive), conflict), on(suspend("audit")), ports.ErrConcurrentUpdate},
		{"retire: unknown node", noSetup, on(retire), ports.ErrNotFound},
		{"retire: proposed", seed(node.DeliveryStation, node.StatusProposed), on(retire), shared.ErrIllegalTransition},
		{"retire: already retired", seed(node.DeliveryStation, node.StatusRetired), on(retire), node.ErrNodeRetired},
		{"retire: conflict twice", both(seed(node.DeliveryStation, node.StatusActive), conflict), on(retire), ports.ErrConcurrentUpdate},
	})
}

func TestLifecycleCommandRetriesOnceOnAConcurrentUpdate(t *testing.T) {
	e := newEnv(t)
	e.seedNode(e.seedRegion("r-1", "SP"), node.DeliveryStation, "n-1", "", node.StatusProposed)
	e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate}
	before := e.before()

	got, err := usecases.NewActivateNode(e.deps).Execute(context.Background(), "n-1")
	wantErr(t, err, nil)

	if got.Status != "ACTIVE" || e.nodes.saves != 2 {
		t.Fatalf("status=%s saves=%d, want ACTIVE after 2 attempts", got.Status, e.nodes.saves)
	}
	e.assertRaised(before, "NodeActivated")
}

func TestUseCaseErrorsStayMatchable(t *testing.T) {
	e := newEnv(t)
	e.outbox.errs = []error{errBoom}
	e.seedNode(e.seedRegion("r-1", "SP"), node.DeliveryStation, "n-1", "", node.StatusProposed)
	_, err := usecases.NewActivateNode(e.deps).Execute(context.Background(), "n-1")
	if !errors.Is(err, errBoom) {
		t.Fatalf("a wrapped outbox failure must keep errors.Is: %v", err)
	}
}
