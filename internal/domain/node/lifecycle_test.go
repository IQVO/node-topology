package node

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

// nodeIn builds a node of the given type in the given state. Fulfillment
// centres get a complete profile first so they can be activated. A SUSPENDED
// node is reached via ACTIVE; retiredFromSuspended picks the RETIRED path.
func nodeIn(t *testing.T, typ Type, status Status, retiredFromSuspended bool) *Node {
	t.Helper()
	in := networkReg(typ)
	if typ == FulfillmentCenter || typ == InboundCrossDock || typ == ReturnsCenter {
		in = vendorReg()
		in.Type, in.Subtype = typ, ""
		if typ == FulfillmentCenter {
			in.Subtype = SubtypeSortable
		}
	}
	n := mustRegister(t, in)
	if typ == FulfillmentCenter {
		d := validDecl()
		if _, err := n.DeclareProfile(d, t0); err != nil {
			t.Fatal(err)
		}
	}
	steps := map[Status][]func() ([]shared.Event, error){
		StatusProposed:  {},
		StatusActive:    {func() ([]shared.Event, error) { return n.Activate(t1) }},
		StatusSuspended: {func() ([]shared.Event, error) { return n.Activate(t1) }, func() ([]shared.Event, error) { return n.Suspend("strike", t2) }},
		StatusRetired:   {func() ([]shared.Event, error) { return n.Activate(t1) }, func() ([]shared.Event, error) { return n.Retire(t3) }},
	}[status]
	if status == StatusRetired && retiredFromSuspended {
		steps = []func() ([]shared.Event, error){
			func() ([]shared.Event, error) { return n.Activate(t1) },
			func() ([]shared.Event, error) { return n.Suspend("strike", t2) },
			func() ([]shared.Event, error) { return n.Retire(t3) },
		}
	}
	for _, step := range steps {
		if _, err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if n.Status() != status {
		t.Fatalf("setup reached %s, want %s", n.Status(), status)
	}
	return n
}

type command struct {
	name string
	run  func(*Node) ([]shared.Event, error)
	to   Status
	// refused maps a starting status to the expected error; absent means allowed.
	refused map[Status]error
}

func commands() []command {
	return []command{
		{"ActivateNode", func(n *Node) ([]shared.Event, error) { return n.Activate(t3) }, StatusActive,
			map[Status]error{StatusActive: shared.ErrIllegalTransition, StatusRetired: ErrNodeRetired}},
		{"SuspendNode", func(n *Node) ([]shared.Event, error) { return n.Suspend("dock fire", t3) }, StatusSuspended,
			map[Status]error{StatusProposed: shared.ErrIllegalTransition, StatusSuspended: shared.ErrIllegalTransition, StatusRetired: ErrNodeRetired}},
		{"RetireNode", func(n *Node) ([]shared.Event, error) { return n.Retire(t3) }, StatusRetired,
			map[Status]error{StatusProposed: shared.ErrIllegalTransition, StatusRetired: ErrNodeRetired}},
	}
}

func TestTransitionMatrixOnEveryNodeType(t *testing.T) {
	states := []Status{StatusProposed, StatusActive, StatusSuspended, StatusRetired}
	for _, typ := range Types() {
		for _, cmd := range commands() {
			for _, from := range states {
				t.Run(string(typ)+"/"+cmd.name+"/from "+string(from), func(t *testing.T) {
					runTransitionCase(t, typ, from, cmd)
				})
			}
		}
	}
}

func runTransitionCase(t *testing.T, typ Type, from Status, cmd command) {
	t.Helper()
	n := nodeIn(t, typ, from, false)
	before, versionBefore := n.Snapshot(), n.Version()
	events, err := cmd.run(n)
	if want, refused := cmd.refused[from]; refused {
		assertRefusedLeftNothing(t, n, before, versionBefore, events, err, want)
		return
	}
	if err != nil {
		t.Fatalf("must be allowed: %v", err)
	}
	if n.Status() != cmd.to || n.Version() != versionBefore+1 || len(events) != 1 {
		t.Fatalf("status %s version %d events %d", n.Status(), n.Version(), len(events))
	}
	if events[0].AggregateVersion() != n.Version() {
		t.Fatal("the event carries the version after the change")
	}
}

func assertRefusedLeftNothing(t *testing.T, n *Node, before Snapshot, versionBefore int64, events []shared.Event, err, want error) {
	t.Helper()
	if !errors.Is(err, want) || events != nil {
		t.Fatalf("want %v with no events, got %v %v", want, err, events)
	}
	if !reflect.DeepEqual(n.Snapshot(), before) || n.Version() != versionBefore {
		t.Fatal("a refused command must change nothing")
	}
}

func TestRefusedTransitionErrorsAreDistinct(t *testing.T) {
	n := nodeIn(t, SortationCenter, StatusRetired, false)
	if _, err := n.Activate(t3); errors.Is(err, shared.ErrIllegalTransition) {
		t.Fatal("a retired node answers ErrNodeRetired, not ErrIllegalTransition")
	}
	p := nodeIn(t, SortationCenter, StatusProposed, false)
	if _, err := p.Suspend("x", t3); errors.Is(err, ErrNodeRetired) {
		t.Fatal("a proposed node answers ErrIllegalTransition, not ErrNodeRetired")
	}
}

func TestActivateEventsAndTimestamps(t *testing.T) {
	n := nodeIn(t, SortationCenter, StatusSuspended, false)
	events, err := n.Activate(t3)
	if err != nil {
		t.Fatal(err)
	}
	want := NodeActivated{Header: shared.Header{Version: n.Version(), At: t3}, NodeID: n.ID(), ActivatedAt: t3}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	assertAccessors(t, events[0], "NodeActivated", string(n.ID()), n.Version(), t3)
	if !n.ActivatedAt().Equal(t3) || !n.SuspendedAt().IsZero() || n.SuspendReason() != "" {
		t.Fatalf("re-activation records the new instant and clears the suspension: %+v", n.Snapshot())
	}
	if n.Version() != 4 {
		t.Fatalf("register, activate, suspend, activate = version 4, got %d", n.Version())
	}
}

func TestSuspendEventAndRecord(t *testing.T) {
	n := nodeIn(t, DeliveryStation, StatusActive, false)
	events, err := n.Suspend("carrier strike", t3)
	if err != nil {
		t.Fatal(err)
	}
	want := NodeSuspended{Header: shared.Header{Version: n.Version(), At: t3}, NodeID: n.ID(), Reason: "carrier strike", SuspendedAt: t3}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	assertAccessors(t, events[0], "NodeSuspended", string(n.ID()), n.Version(), t3)
	if !n.SuspendedAt().Equal(t3) || n.SuspendReason() != "carrier strike" || !n.ActivatedAt().Equal(t1) {
		t.Fatalf("suspension not recorded: %+v", n.Snapshot())
	}
}

func TestSuspendRequiresAReason(t *testing.T) {
	for _, reason := range []string{"", "   ", " leading", "trailing ", "multi\nline", strings.Repeat("r", MaxReason+1)} {
		n := nodeIn(t, DeliveryStation, StatusActive, false)
		events, err := n.Suspend(reason, t3)
		if !errors.Is(err, shared.ErrValidation) || events != nil || n.Status() != StatusActive || n.Version() != 2 {
			t.Errorf("reason %q: want ErrValidation and no change, got %v (status %s version %d)", reason, err, n.Status(), n.Version())
		}
	}
	n := nodeIn(t, DeliveryStation, StatusActive, false)
	if _, err := n.Suspend(strings.Repeat("r", MaxReason), t3); err != nil {
		t.Fatalf("a reason at the limit is fine: %v", err)
	}
}

func TestRetireEventsAndRetirementKeepsSuspensionRecord(t *testing.T) {
	n := nodeIn(t, AirHub, StatusSuspended, false)
	events, err := n.Retire(t3)
	if err != nil {
		t.Fatal(err)
	}
	want := NodeRetired{Header: shared.Header{Version: n.Version(), At: t3}, NodeID: n.ID(), RetiredAt: t3}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	assertAccessors(t, events[0], "NodeRetired", string(n.ID()), n.Version(), t3)
	if !n.RetiredAt().Equal(t3) || n.SuspendReason() != "strike" || !n.SuspendedAt().Equal(t2) {
		t.Fatalf("retiring from SUSPENDED keeps the suspension: %+v", n.Snapshot())
	}
	if _, err := Restore(n.Snapshot()); err != nil {
		t.Fatalf("a node retired from SUSPENDED must restore: %v", err)
	}
}

func TestAcceptsDemandOnlyWhenActiveWithOutbound(t *testing.T) {
	for _, typ := range Types() {
		for _, st := range []Status{StatusProposed, StatusActive, StatusSuspended, StatusRetired} {
			n := nodeIn(t, typ, st, false)
			want := st == StatusActive && typ == FulfillmentCenter
			if got := n.AcceptsDemand(); got != want {
				t.Errorf("%s %s: AcceptsDemand() = %v, want %v", typ, st, got, want)
			}
		}
	}
}

func TestActivationOfFulfillmentCenterNeedsACompleteProfile(t *testing.T) {
	tests := []struct {
		name string
		decl *ProfileDeclaration
	}{
		{"no profile", nil},
		{"no cutoff", ptr(decl(func(d *ProfileDeclaration) { d.Cutoffs = nil }))},
		{"no handling time", ptr(decl(func(d *ProfileDeclaration) { d.HandlingTimeMinutes = 0 }))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := mustRegister(t, vendorReg())
			if tc.decl != nil {
				if _, err := n.DeclareProfile(*tc.decl, t0); err != nil {
					t.Fatalf("a PROPOSED node accepts an incomplete profile: %v", err)
				}
			}
			events, err := n.Activate(t1)
			if !errors.Is(err, ErrIncompleteOperationalProfile) || events != nil {
				t.Fatalf("want ErrIncompleteOperationalProfile, got %v", err)
			}
			if n.Status() != StatusProposed || n.Version() != 1+boolToInt(tc.decl != nil) {
				t.Fatalf("a refused activation changes nothing: %s v%d", n.Status(), n.Version())
			}
		})
	}
	// one cutoff and one minute are enough
	n := mustRegister(t, vendorReg())
	minimal := decl(func(d *ProfileDeclaration) { d.Cutoffs, d.HandlingTimeMinutes = []CutoffSlot{{LocalTime: "10:00"}}, 1 })
	if _, err := n.DeclareProfile(minimal, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Activate(t1); err != nil {
		t.Fatalf("one cutoff and one minute suffice: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func TestNonFulfillmentNodesActivateWithoutAProfile(t *testing.T) {
	for _, typ := range Types() {
		if typ == FulfillmentCenter {
			continue
		}
		n := nodeIn(t, typ, StatusProposed, false)
		if _, ok := n.Profile(); ok {
			t.Fatalf("%s: setup must have no profile", typ)
		}
		if _, err := n.Activate(t1); err != nil {
			t.Errorf("%s activates without a profile: %v", typ, err)
		}
	}
}

func TestReactivationRechecksCompleteness(t *testing.T) {
	n := nodeIn(t, FulfillmentCenter, StatusActive, false)
	if _, err := n.Suspend("audit", t2); err != nil {
		t.Fatal(err)
	}
	incomplete := decl(func(d *ProfileDeclaration) { d.Revision, d.Cutoffs = 2, nil })
	if _, err := n.DeclareProfile(incomplete, t2); err != nil {
		t.Fatalf("a SUSPENDED fulfillment centre may hold an incomplete profile: %v", err)
	}
	if _, err := n.Activate(t3); !errors.Is(err, ErrIncompleteOperationalProfile) {
		t.Fatalf("re-activation re-checks the current profile: %v", err)
	}
}
