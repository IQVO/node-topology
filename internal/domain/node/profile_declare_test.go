package node

import (
	"errors"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

func TestDeclareProfileEventAndRevision(t *testing.T) {
	n := mustRegister(t, vendorReg())
	events, err := n.DeclareProfile(validDecl(), t1)
	if err != nil {
		t.Fatal(err)
	}
	d := validDecl()
	want := NodeOperationalProfileDeclared{
		Header: shared.Header{Version: 2, At: t1}, NodeID: "node-0f3a91", ProfileRevision: 1, Timezone: "America/Sao_Paulo",
		OperatingHoursByDay: d.OperatingHours, Holidays: d.Holidays, HandlingTimeMinutes: 240, Cutoffs: d.Cutoffs,
		ThroughputCap: d.ThroughputCap, DeclaredBy: DeclaredByVendor,
	}
	if len(events) != 1 || !reflect.DeepEqual(events[0], want) {
		t.Fatalf("events = %+v\nwant     %+v", events, want)
	}
	assertAccessors(t, events[0], "NodeOperationalProfileDeclared", "node-0f3a91", 2, t1)
	p, ok := n.Profile()
	if !ok || p.Revision != 1 || n.ProfileRevision() != 1 || !p.DeclaredAt.Equal(t1) || n.Status() != StatusProposed {
		t.Fatalf("profile not stored: %+v", p)
	}
	// the event does not alias the aggregate
	events[0].(NodeOperationalProfileDeclared).Cutoffs[0].LocalTime = "00:00"
	events[0].(NodeOperationalProfileDeclared).OperatingHoursByDay[Monday] = DayHours{}
	if again, _ := n.Profile(); again.Cutoffs[0].LocalTime != "11:00" || again.OperatingHours[Monday].Start != "06:00" {
		t.Fatal("the event aliased the stored profile")
	}
	// Profile() returns a copy
	p.Cutoffs[0].LocalTime = "00:00"
	if again, _ := n.Profile(); again.Cutoffs[0].LocalTime != "11:00" {
		t.Fatal("Profile() must return a copy")
	}
}

func TestDeclareProfileRevisionsStrictlyIncrease(t *testing.T) {
	n := mustRegister(t, vendorReg())
	rev := func(r int) ProfileDeclaration { return decl(func(d *ProfileDeclaration) { d.Revision = r }) }
	if _, err := n.DeclareProfile(rev(5), t1); err != nil {
		t.Fatalf("a first revision need not be 1: %v", err)
	}
	for _, r := range []int{-1, 0, 1, 4, 5} {
		before := n.Snapshot()
		events, err := n.DeclareProfile(rev(r), t2)
		if !errors.Is(err, ErrStaleProfileRevision) || events != nil {
			t.Errorf("revision %d: want ErrStaleProfileRevision, got %v", r, err)
		}
		if !reflect.DeepEqual(n.Snapshot(), before) {
			t.Errorf("revision %d: a refused declaration changes nothing", r)
		}
	}
	if _, err := n.DeclareProfile(rev(6), t2); err != nil {
		t.Fatalf("current+1 is accepted: %v", err)
	}
	if n.ProfileRevision() != 6 || n.Version() != 3 {
		t.Fatalf("revision %d version %d", n.ProfileRevision(), n.Version())
	}
	// revision zero on a fresh node is stale as well (the first revision is at least 1)
	fresh := mustRegister(t, vendorReg())
	if _, err := fresh.DeclareProfile(rev(0), t1); !errors.Is(err, ErrStaleProfileRevision) {
		t.Fatalf("revision 0: %v", err)
	}
}

func TestDeclareProfileInEveryNonRetiredState(t *testing.T) {
	for _, st := range []Status{StatusProposed, StatusActive, StatusSuspended} {
		n := nodeIn(t, FulfillmentCenter, st, false)
		if _, err := n.DeclareProfile(decl(func(d *ProfileDeclaration) { d.Revision = 2 }), t3); err != nil {
			t.Errorf("%s: %v", st, err)
		}
		if n.Status() != st {
			t.Errorf("%s: declaring a profile changes no state, got %s", st, n.Status())
		}
	}
	for _, fromSuspended := range []bool{false, true} {
		n := nodeIn(t, FulfillmentCenter, StatusRetired, fromSuspended)
		before := n.Snapshot()
		events, err := n.DeclareProfile(decl(func(d *ProfileDeclaration) { d.Revision = 2 }), t3)
		if !errors.Is(err, ErrNodeRetired) || events != nil || !reflect.DeepEqual(n.Snapshot(), before) {
			t.Errorf("retired: want ErrNodeRetired and no change, got %v", err)
		}
	}
}

func TestDeclareProfileCheckOrder(t *testing.T) {
	// retired beats stale
	n := nodeIn(t, FulfillmentCenter, StatusRetired, false)
	if _, err := n.DeclareProfile(validDecl(), t3); !errors.Is(err, ErrNodeRetired) {
		t.Fatalf("retired first: %v", err)
	}
	// stale beats a malformed profile
	n = nodeIn(t, FulfillmentCenter, StatusActive, false)
	bad := decl(func(d *ProfileDeclaration) { d.ThroughputCap.Value = 0 })
	if _, err := n.DeclareProfile(bad, t3); !errors.Is(err, ErrStaleProfileRevision) {
		t.Fatalf("stale before shape: %v", err)
	}
	// a cutoff outside the hours beats incompleteness
	outside := decl(func(d *ProfileDeclaration) {
		d.Revision, d.HandlingTimeMinutes, d.Cutoffs = 2, 0, []CutoffSlot{{LocalTime: "03:00"}}
	})
	if _, err := n.DeclareProfile(outside, t3); !errors.Is(err, ErrCutoffOutsideOperatingHours) {
		t.Fatalf("cutoff window before completeness: %v", err)
	}
	// incompleteness beats sort dwell on an ACTIVE fulfillment centre
	both := decl(func(d *ProfileDeclaration) { d.Revision, d.Cutoffs, d.SortDwellMinutes = 2, nil, intp(10) })
	if _, err := n.DeclareProfile(both, t3); !errors.Is(err, ErrIncompleteOperationalProfile) {
		t.Fatalf("completeness before sort dwell: %v", err)
	}
	// sort dwell alone
	dwell := decl(func(d *ProfileDeclaration) { d.Revision, d.SortDwellMinutes = 2, intp(10) })
	if _, err := n.DeclareProfile(dwell, t3); !errors.Is(err, ErrSortDwellNotAllowed) {
		t.Fatalf("sort dwell on a fulfillment centre: %v", err)
	}
}

func TestActiveFulfillmentCenterKeepsItsProfileComplete(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*ProfileDeclaration)
	}{
		{"drops every cutoff", func(d *ProfileDeclaration) { d.Cutoffs = nil }},
		{"zero handling time", func(d *ProfileDeclaration) { d.HandlingTimeMinutes = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := nodeIn(t, FulfillmentCenter, StatusActive, false)
			d := decl(tc.mut)
			d.Revision = 2
			before := n.Snapshot()
			events, err := n.DeclareProfile(d, t3)
			if !errors.Is(err, ErrIncompleteOperationalProfile) || events != nil || !reflect.DeepEqual(n.Snapshot(), before) {
				t.Fatalf("want ErrIncompleteOperationalProfile and no change, got %v", err)
			}
		})
	}
	// the rule is about ACTIVE fulfillment centres only
	other := nodeIn(t, SortationCenter, StatusActive, false)
	empty := decl(func(d *ProfileDeclaration) {
		d.Cutoffs, d.HandlingTimeMinutes, d.DeclaredBy = nil, 0, DeclaredByNetworkOps
	})
	if _, err := other.DeclareProfile(empty, t3); err != nil {
		t.Fatalf("a sortation centre needs no cutoff: %v", err)
	}
}

func TestSortDwellOnlyForSortingNodes(t *testing.T) {
	for _, typ := range Types() {
		n := nodeIn(t, typ, StatusProposed, false)
		d := decl(func(d *ProfileDeclaration) { d.SortDwellMinutes, d.Revision = intp(0), n.ProfileRevision()+1 })
		if n.Operator() == OperatorNetwork {
			d.DeclaredBy = DeclaredByNetworkOps
		}
		events, err := n.DeclareProfile(d, t1)
		sorts := typ == SortationCenter || typ == AirHub
		if sorts {
			if err != nil || *events[0].(NodeOperationalProfileDeclared).SortDwellMinutes != 0 {
				t.Errorf("%s declares a sort dwell: %v", typ, err)
			}
			continue
		}
		if !errors.Is(err, ErrSortDwellNotAllowed) || events != nil {
			t.Errorf("%s: want ErrSortDwellNotAllowed, got %v", typ, err)
		}
	}
}

func TestDeclaredByVendorIsRefusedOnNetworkNodes(t *testing.T) {
	for _, typ := range []Type{SortationCenter, DeliveryStation, AirHub} {
		n := nodeIn(t, typ, StatusProposed, false)
		if _, err := n.DeclareProfile(validDecl(), t1); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%s: VENDOR may not declare a network node: %v", typ, err)
		}
		ops := decl(func(d *ProfileDeclaration) { d.DeclaredBy = DeclaredByNetworkOps })
		if _, err := n.DeclareProfile(ops, t1); err != nil {
			t.Errorf("%s: NETWORK_OPS may: %v", typ, err)
		}
	}
	// NETWORK_OPS may also declare a vendor node
	v := mustRegister(t, vendorReg())
	if _, err := v.DeclareProfile(decl(func(d *ProfileDeclaration) { d.DeclaredBy = DeclaredByNetworkOps }), t1); err != nil {
		t.Fatalf("NETWORK_OPS declares a vendor node: %v", err)
	}
}

func TestDeclareProfileValueErrorsAreValidation(t *testing.T) {
	n := mustRegister(t, vendorReg())
	bad := decl(func(d *ProfileDeclaration) { d.Timezone = "" })
	if _, err := n.DeclareProfile(bad, t1); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if n.Version() != 1 || n.ProfileRevision() != 0 {
		t.Fatal("a refused declaration changes nothing")
	}
}

func TestProfileDeclarationDoesNotAliasTheCaller(t *testing.T) {
	n := mustRegister(t, vendorReg())
	d := validDecl()
	if _, err := n.DeclareProfile(d, t1); err != nil {
		t.Fatal(err)
	}
	d.OperatingHours[Monday] = DayHours{Start: "01:00", End: "02:00"}
	d.Cutoffs[0].LocalTime = "00:00"
	d.Holidays[0] = "2000-01-01"
	p, _ := n.Profile()
	if p.OperatingHours[Monday].Start != "06:00" || p.Cutoffs[0].LocalTime != "11:00" || p.Holidays[0] != "2026-12-25" {
		t.Fatalf("the node aliased the caller's declaration: %+v", p)
	}
}
