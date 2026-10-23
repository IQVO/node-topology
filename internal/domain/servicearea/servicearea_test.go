package servicearea

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

var (
	t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t0.Add(2 * time.Hour)
)

func mkRegion(t *testing.T, id string) *region.Region {
	t.Helper()
	r, _, err := region.Define(region.NewRegion{ID: region.ID(id), Code: strings.ToUpper(id), Name: id}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mkNode(t *testing.T, id string, typ node.Type, regionID string, status node.Status) *node.Node {
	t.Helper()
	in := node.Registration{
		ID: node.ID(id), Type: typ, Operator: node.OperatorNetwork, RegionID: region.ID(regionID), Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}
	if typ.AllowsOperator(node.OperatorVendor) {
		in.Operator = node.OperatorVendor
		in.Vendor = &node.VendorRef{VendorID: "vendor-77", VendorNodeCode: "code-" + id}
	}
	n, _, err := node.Register(in, mkRegion(t, regionID), t0)
	if err != nil {
		t.Fatal(err)
	}
	if typ == node.FulfillmentCenter && status != node.StatusProposed {
		d := node.ProfileDeclaration{
			Revision: 1, Timezone: "America/Sao_Paulo", HandlingTimeMinutes: 60,
			OperatingHours: node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
			Cutoffs:        []node.CutoffSlot{{LocalTime: "11:00"}},
			ThroughputCap:  node.ThroughputCap{Value: 10, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
			DeclaredBy:     node.DeclaredByVendor,
		}
		if _, err := n.DeclareProfile(d, t0); err != nil {
			t.Fatal(err)
		}
	}
	if status == node.StatusProposed {
		return n
	}
	if _, err := n.Activate(t0); err != nil {
		t.Fatal(err)
	}
	switch status {
	case node.StatusSuspended:
		_, err = n.Suspend("test", t0)
	case node.StatusRetired:
		_, err = n.Retire(t0)
	}
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func station(t *testing.T) *node.Node {
	return mkNode(t, "node-9b7e02", node.DeliveryStation, "region-se", node.StatusActive)
}

func firstAssignment(t *testing.T) *ServiceArea {
	t.Helper()
	sa, _, err := Assign("01310", 1, station(t), mkRegion(t, "region-se"), t0)
	if err != nil {
		t.Fatal(err)
	}
	return sa
}

func TestAssignFirstAssignment(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	sa, events, err := Assign("01310", 1, station(t), mkRegion(t, "region-se"), t0.In(brt))
	if err != nil {
		t.Fatal(err)
	}
	if sa.PostalPrefix() != "01310" || sa.DeliveryStationNodeID() != "node-9b7e02" || sa.RegionID() != "region-se" ||
		sa.AssignmentVersion() != 1 || sa.Version() != 1 || !sa.AssignedAt().Equal(t0) || sa.AssignedAt().Location() != time.UTC {
		t.Fatalf("unexpected service area: %+v", sa.Snapshot())
	}
	want := ServiceAreaAssigned{
		Header: shared.Header{Version: 1, At: t0}, PostalPrefix: "01310", DeliveryStationNodeID: "node-9b7e02",
		RegionID: "region-se", AssignmentVersion: 1,
	}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	ev := events[0]
	if ev.EventName() != "ServiceAreaAssigned" || ev.Entity() != "servicearea" || ev.SubjectID() != "01310" || ev.AggregateVersion() != 1 || !ev.OccurredAt().Equal(t0) {
		t.Fatalf("accessors wrong: %+v", ev)
	}
}

func TestAssignRejectionsOnFirstAssignment(t *testing.T) {
	se := func(t *testing.T) *region.Region { return mkRegion(t, "region-se") }
	tests := []struct {
		name    string
		prefix  PostalPrefix
		version int
		station func(*testing.T) *node.Node
		region  func(*testing.T) *region.Region
		want    error
	}{
		{"empty prefix", "", 1, station, se, shared.ErrValidation},
		{"lowercase prefix", "sw1a", 1, station, se, shared.ErrValidation},
		{"prefix with space", "01 310", 1, station, se, shared.ErrValidation},
		{"prefix too long", PostalPrefix(strings.Repeat("1", MaxPrefix+1)), 1, station, se, shared.ErrValidation},
		{"version zero", "01310", 0, station, se, shared.ErrValidation},
		{"version negative", "01310", -1, station, se, shared.ErrValidation},
		{"version above int32", "01310", MaxAssignmentVersion + 1, station, se, shared.ErrValidation},
		{"station not loaded", "01310", 1, func(*testing.T) *node.Node { return nil }, se, ErrNotADeliveryStation},
		{"sortation centre", "01310", 1, func(t *testing.T) *node.Node {
			return mkNode(t, "n1", node.SortationCenter, "region-se", node.StatusActive)
		}, se, ErrNotADeliveryStation},
		{"fulfillment centre", "01310", 1, func(t *testing.T) *node.Node {
			return mkNode(t, "n1", node.FulfillmentCenter, "region-se", node.StatusActive)
		}, se, ErrNotADeliveryStation},
		{"air hub", "01310", 1, func(t *testing.T) *node.Node { return mkNode(t, "n1", node.AirHub, "region-se", node.StatusActive) }, se, ErrNotADeliveryStation},
		{"proposed station", "01310", 1, func(t *testing.T) *node.Node {
			return mkNode(t, "n1", node.DeliveryStation, "region-se", node.StatusProposed)
		}, se, ErrNotADeliveryStation},
		{"suspended station", "01310", 1, func(t *testing.T) *node.Node {
			return mkNode(t, "n1", node.DeliveryStation, "region-se", node.StatusSuspended)
		}, se, ErrNotADeliveryStation},
		{"retired station", "01310", 1, func(t *testing.T) *node.Node {
			return mkNode(t, "n1", node.DeliveryStation, "region-se", node.StatusRetired)
		}, se, ErrNotADeliveryStation},
		{"region not loaded", "01310", 1, station, func(*testing.T) *region.Region { return nil }, region.ErrUnknownRegion},
		{"region differs from the station's", "01310", 1, station, func(t *testing.T) *region.Region { return mkRegion(t, "region-ne") }, region.ErrUnknownRegion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sa, events, err := Assign(tc.prefix, tc.version, tc.station(t), tc.region(t), t0)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if sa != nil || events != nil {
				t.Fatal("nothing may be returned on error")
			}
		})
	}
}

func TestAssignBoundaryValues(t *testing.T) {
	for _, v := range []int{1, MaxAssignmentVersion} {
		if _, _, err := Assign("01310", v, station(t), mkRegion(t, "region-se"), t0); err != nil {
			t.Errorf("version %d must be accepted: %v", v, err)
		}
	}
	for _, p := range []PostalPrefix{"0", "A", "Z", "9", "-", "SW1A-1AA", PostalPrefix(strings.Repeat("9", MaxPrefix))} {
		if !p.Valid() {
			t.Errorf("%q must be a valid prefix", p)
		}
	}
	for _, p := range []PostalPrefix{"/", ":", "@", "[", "a", "z", ",", ".", " ", "_", "é", "0/", "9:", "A@", "Z["} {
		if p.Valid() {
			t.Errorf("%q must be an invalid prefix", p)
		}
	}
}

func TestAssignCheckOrder(t *testing.T) {
	// values beat the station
	if _, _, err := Assign("", 1, nil, nil, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("values first: %v", err)
	}
	// the station beats a stale version and the region
	sa := firstAssignment(t)
	bad := mkNode(t, "n1", node.SortationCenter, "region-se", node.StatusActive)
	if _, err := sa.Reassign(1, bad, nil, t1); !errors.Is(err, ErrNotADeliveryStation) {
		t.Fatalf("station before version: %v", err)
	}
	// the version beats the region
	if _, err := sa.Reassign(1, station(t), nil, t1); !errors.Is(err, ErrStaleAssignmentVersion) {
		t.Fatalf("version before region: %v", err)
	}
}

func TestReassignEmitsTheNewAssignment(t *testing.T) {
	sa := firstAssignment(t)
	other := mkNode(t, "node-other", node.DeliveryStation, "region-ne", node.StatusActive)
	events, err := sa.Reassign(2, other, mkRegion(t, "region-ne"), t1)
	if err != nil {
		t.Fatal(err)
	}
	want := ServiceAreaAssigned{
		Header: shared.Header{Version: 2, At: t1}, PostalPrefix: "01310", DeliveryStationNodeID: "node-other",
		RegionID: "region-ne", AssignmentVersion: 2,
	}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	if sa.DeliveryStationNodeID() != "node-other" || sa.RegionID() != "region-ne" || sa.AssignmentVersion() != 2 || sa.Version() != 2 || !sa.AssignedAt().Equal(t1) {
		t.Fatalf("unexpected service area: %+v", sa.Snapshot())
	}
}

func TestReassignRefusesNonIncreasingVersions(t *testing.T) {
	sa := firstAssignment(t)
	other := mkNode(t, "node-other", node.DeliveryStation, "region-ne", node.StatusActive)
	if _, err := sa.Reassign(2, other, mkRegion(t, "region-ne"), t1); err != nil {
		t.Fatal(err)
	}
	for _, v := range []int{-1, 0, 1, 2} {
		before := sa.Snapshot()
		events, err := sa.Reassign(v, other, mkRegion(t, "region-ne"), t2)
		wantErr := ErrStaleAssignmentVersion
		if v < 1 {
			wantErr = shared.ErrValidation
		}
		if !errors.Is(err, wantErr) || events != nil || !reflect.DeepEqual(sa.Snapshot(), before) {
			t.Errorf("version %d: want %v with no change, got %v", v, wantErr, err)
		}
	}
}

func TestReassignMayJumpVersionsAndNameTheSameStationAgain(t *testing.T) {
	sa := firstAssignment(t)
	other := mkNode(t, "node-other", node.DeliveryStation, "region-ne", node.StatusActive)
	if _, err := sa.Reassign(2, other, mkRegion(t, "region-ne"), t1); err != nil {
		t.Fatal(err)
	}
	if _, err := sa.Reassign(10, other, mkRegion(t, "region-ne"), t2); err != nil {
		t.Fatalf("a higher version is enough: %v", err)
	}
	if sa.AssignmentVersion() != 10 || sa.Version() != 3 {
		t.Fatalf("assignment %d version %d", sa.AssignmentVersion(), sa.Version())
	}
}

func TestReassignRefusalsLeaveTheAssignmentAlone(t *testing.T) {
	sa := firstAssignment(t)
	before := sa.Snapshot()
	suspended := mkNode(t, "node-susp", node.DeliveryStation, "region-se", node.StatusSuspended)
	if events, err := sa.Reassign(2, suspended, mkRegion(t, "region-se"), t1); !errors.Is(err, ErrNotADeliveryStation) || events != nil {
		t.Fatalf("want ErrNotADeliveryStation, got %v", err)
	}
	if events, err := sa.Reassign(2, station(t), mkRegion(t, "region-ne"), t1); !errors.Is(err, region.ErrUnknownRegion) || events != nil {
		t.Fatalf("want ErrUnknownRegion, got %v", err)
	}
	if !reflect.DeepEqual(sa.Snapshot(), before) {
		t.Fatal("a refused reassignment changes nothing")
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	sa := firstAssignment(t)
	snap := sa.Snapshot()
	want := Snapshot{PostalPrefix: "01310", DeliveryStationNodeID: "node-9b7e02", RegionID: "region-se", AssignmentVersion: 1, AssignedAt: t0, Version: 1}
	if snap != want {
		t.Fatalf("snapshot = %+v", snap)
	}
	back, err := Restore(snap)
	if err != nil || back.Snapshot() != snap {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	brt := time.FixedZone("BRT", -3*3600)
	snap.AssignedAt = snap.AssignedAt.In(brt)
	back, err = Restore(snap)
	if err != nil || back.AssignedAt().Location() != time.UTC {
		t.Fatalf("restored instants are UTC: %v", err)
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	good := firstAssignment(t).Snapshot()
	tests := []struct {
		name string
		mut  func(*Snapshot)
	}{
		{"bad prefix", func(s *Snapshot) { s.PostalPrefix = "" }},
		{"bad station id", func(s *Snapshot) { s.DeliveryStationNodeID = "" }},
		{"bad region id", func(s *Snapshot) { s.RegionID = "a b" }},
		{"version zero", func(s *Snapshot) { s.AssignmentVersion = 0 }},
		{"version too large", func(s *Snapshot) { s.AssignmentVersion = MaxAssignmentVersion + 1 }},
		{"no assignedAt", func(s *Snapshot) { s.AssignedAt = time.Time{} }},
		{"aggregate version zero", func(s *Snapshot) { s.Version = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := good
			tc.mut(&s)
			sa, err := Restore(s)
			if !errors.Is(err, shared.ErrInvalidSnapshot) || sa != nil {
				t.Fatalf("want ErrInvalidSnapshot, got %v", err)
			}
		})
	}
}

func TestRestoreAcceptsTheVersionRangeEdges(t *testing.T) {
	for _, v := range []int{1, MaxAssignmentVersion} {
		s := firstAssignment(t).Snapshot()
		s.AssignmentVersion = v
		sa, err := Restore(s)
		if err != nil || sa.Snapshot().AssignmentVersion != v {
			t.Fatalf("version %d must restore: %v", v, err)
		}
	}
}
