package node

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

func TestSnapshotRestoreRoundTripInEveryState(t *testing.T) {
	for _, typ := range Types() {
		for _, st := range []Status{StatusProposed, StatusActive, StatusSuspended, StatusRetired} {
			assertRoundTrip(t, typ, st, false)
			if st == StatusRetired {
				assertRoundTrip(t, typ, st, true)
			}
		}
	}
}

func assertRoundTrip(t *testing.T, typ Type, st Status, fromSuspended bool) {
	t.Helper()
	n := nodeIn(t, typ, st, fromSuspended)
	snap := n.Snapshot()
	back, err := Restore(snap)
	if err != nil {
		t.Fatalf("%s %s: %v", typ, st, err)
	}
	if !reflect.DeepEqual(back.Snapshot(), snap) {
		t.Fatalf("%s %s: round trip changed the node:\n%+v\n%+v", typ, st, back.Snapshot(), snap)
	}
	if back.Version() != n.Version() || back.Status() != st {
		t.Fatalf("%s %s: version/status lost", typ, st)
	}
}

func TestRestoreNormalisesToUTCAndCopies(t *testing.T) {
	n := nodeIn(t, FulfillmentCenter, StatusSuspended, false)
	snap := n.Snapshot()
	brt := time.FixedZone("BRT", -3*3600)
	snap.ActivatedAt, snap.SuspendedAt = snap.ActivatedAt.In(brt), snap.SuspendedAt.In(brt)
	back, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if back.ActivatedAt().Location() != time.UTC || back.SuspendedAt().Location() != time.UTC {
		t.Fatal("restored instants are UTC")
	}
	snap.Vendor.VendorID = "changed"
	snap.Profile.Cutoffs[0].LocalTime = "00:00"
	*snap.Location.Lat = 0
	if v, _ := back.Vendor(); v.VendorID != "vendor-77" {
		t.Fatal("Restore aliased the vendor")
	}
	if p, _ := back.Profile(); p.Cutoffs[0].LocalTime != "11:00" {
		t.Fatal("Restore aliased the profile")
	}
	if *back.Location().Lat != -23.35 {
		t.Fatal("Restore aliased the location")
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	active := func(t *testing.T) Snapshot { return nodeIn(t, FulfillmentCenter, StatusActive, false).Snapshot() }
	suspended := func(t *testing.T) Snapshot { return nodeIn(t, FulfillmentCenter, StatusSuspended, false).Snapshot() }
	retired := func(t *testing.T) Snapshot { return nodeIn(t, FulfillmentCenter, StatusRetired, false).Snapshot() }
	proposed := func(t *testing.T) Snapshot { return nodeIn(t, FulfillmentCenter, StatusProposed, false).Snapshot() }
	network := func(t *testing.T) Snapshot { return nodeIn(t, SortationCenter, StatusActive, false).Snapshot() }
	tests := []struct {
		name  string
		base  func(*testing.T) Snapshot
		mut   func(*Snapshot)
		cause error
	}{
		{"unknown type", active, func(s *Snapshot) { s.Type = "WAREHOUSE" }, nil},
		{"unknown operator", active, func(s *Snapshot) { s.Operator = "PARTNER" }, nil},
		{"operator not allowed", active, func(s *Snapshot) { s.Operator = OperatorNetwork; s.Vendor = nil }, ErrOperatorNotAllowedForNodeType},
		{"bad id", active, func(s *Snapshot) { s.ID = "" }, nil},
		{"bad region id", active, func(s *Snapshot) { s.RegionID = "" }, nil},
		{"vendor node without reference", active, func(s *Snapshot) { s.Vendor = nil }, nil},
		{"network node with reference", network, func(s *Snapshot) { s.Vendor = &VendorRef{VendorID: "v", VendorNodeCode: "c"} }, nil},
		{"bad timezone", active, func(s *Snapshot) { s.Timezone = "" }, nil},
		{"bad location", active, func(s *Snapshot) { s.Location.CountryCode = "br" }, nil},
		{"subtype not allowed", network, func(s *Snapshot) { s.Subtype = SubtypeXL }, ErrSubtypeNotAllowed},
		{"version zero", active, func(s *Snapshot) { s.Version = 0 }, nil},
		{"unknown status", active, func(s *Snapshot) { s.Status = "DRAFT" }, nil},
		{"proposed with activatedAt", proposed, func(s *Snapshot) { s.ActivatedAt = t1 }, nil},
		{"proposed with retiredAt", proposed, func(s *Snapshot) { s.RetiredAt = t1 }, nil},
		{"proposed with a suspension reason", proposed, func(s *Snapshot) { s.SuspendReason = "x" }, nil},
		{"proposed with suspendedAt", proposed, func(s *Snapshot) { s.SuspendedAt = t1 }, nil},
		{"active without activatedAt", active, func(s *Snapshot) { s.ActivatedAt = time.Time{} }, nil},
		{"active with retiredAt", active, func(s *Snapshot) { s.RetiredAt = t3 }, nil},
		{"active with a suspension", active, func(s *Snapshot) { s.SuspendedAt, s.SuspendReason = t2, "x" }, nil},
		{"active with a stray reason", active, func(s *Snapshot) { s.SuspendReason = "x" }, nil},
		{"suspended without suspendedAt", suspended, func(s *Snapshot) { s.SuspendedAt = time.Time{} }, nil},
		{"suspended without reason", suspended, func(s *Snapshot) { s.SuspendReason = "" }, nil},
		{"suspended with a blank reason", suspended, func(s *Snapshot) { s.SuspendReason = "  " }, nil},
		{"suspended with retiredAt", suspended, func(s *Snapshot) { s.RetiredAt = t3 }, nil},
		{"suspended without activatedAt", suspended, func(s *Snapshot) { s.ActivatedAt = time.Time{} }, nil},
		{"retired without retiredAt", retired, func(s *Snapshot) { s.RetiredAt = time.Time{} }, nil},
		{"retired without activatedAt", retired, func(s *Snapshot) { s.ActivatedAt = time.Time{} }, nil},
		{"retired with a half suspension", retired, func(s *Snapshot) { s.SuspendReason = "x" }, nil},
		{"active fulfillment centre without a profile", active, func(s *Snapshot) { s.Profile = nil }, ErrIncompleteOperationalProfile},
		{"active fulfillment centre with an incomplete profile", active, func(s *Snapshot) { s.Profile.Cutoffs = nil }, ErrIncompleteOperationalProfile},
		{"profile with bad shape", active, func(s *Snapshot) { s.Profile.ThroughputCap.Value = 0 }, nil},
		{"profile with cutoff outside the hours", active, func(s *Snapshot) { s.Profile.Cutoffs = []CutoffSlot{{LocalTime: "03:00"}} }, ErrCutoffOutsideOperatingHours},
		{"profile with sort dwell on a fulfillment centre", active, func(s *Snapshot) { s.Profile.SortDwellMinutes = intp(5) }, ErrSortDwellNotAllowed},
		{"vendor-declared profile on a network node", network, func(s *Snapshot) {
			p := validProfile(t0)
			s.Profile = &p
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.base(t)
			tc.mut(&s)
			n, err := Restore(s)
			if !errors.Is(err, shared.ErrInvalidSnapshot) || n != nil {
				t.Fatalf("want ErrInvalidSnapshot, got %v", err)
			}
			if errors.Is(err, shared.ErrValidation) {
				t.Fatal("a corrupt row is not a client error")
			}
			if !strings.Contains(err.Error(), string(s.ID)) && s.ID != "" {
				t.Fatalf("the error should name the node: %v", err)
			}
		})
	}
}

func validProfile(at time.Time) OperationalProfile {
	p, err := NewOperationalProfile(validDecl(), at)
	if err != nil {
		panic(err)
	}
	return p
}

func TestRestoreAcceptsAFulfillmentCentreWithoutAProfileBeforeActivation(t *testing.T) {
	s := nodeIn(t, FulfillmentCenter, StatusProposed, false).Snapshot()
	s.Profile = nil
	if _, err := Restore(s); err != nil {
		t.Fatal(err)
	}
	// a SUSPENDED fulfillment centre may hold an incomplete profile
	susp := nodeIn(t, FulfillmentCenter, StatusSuspended, false).Snapshot()
	susp.Profile.Cutoffs = nil
	if _, err := Restore(susp); err != nil {
		t.Fatal(err)
	}
	// a non-fulfillment node may be ACTIVE without a profile
	net := nodeIn(t, DeliveryStation, StatusActive, false).Snapshot()
	if net.Profile != nil {
		t.Fatal("setup")
	}
	if _, err := Restore(net); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreCountryCodeLetterEdges(t *testing.T) {
	for _, cc := range []string{"AA", "ZZ", "AZ", "ZA"} {
		s := nodeIn(t, FulfillmentCenter, StatusActive, false).Snapshot()
		s.Location.CountryCode = cc
		if _, err := Restore(s); err != nil {
			t.Fatalf("country %q must restore: %v", cc, err)
		}
	}
	for _, cc := range []string{"@A", "[A", "A@", "A[", "a", "AAA"} {
		s := nodeIn(t, FulfillmentCenter, StatusActive, false).Snapshot()
		s.Location.CountryCode = cc
		if _, err := Restore(s); err == nil {
			t.Fatalf("country %q must be refused", cc)
		}
	}
}
