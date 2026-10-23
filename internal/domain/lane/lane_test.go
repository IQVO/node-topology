package lane

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

func se(t *testing.T) *region.Region {
	t.Helper()
	r, _, err := region.Define(region.NewRegion{ID: "region-se", Code: "SE", Name: "Southeast"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// mkNode builds a node of typ with the given id in the given status.
func mkNode(t *testing.T, id string, typ node.Type, status node.Status) *node.Node {
	t.Helper()
	in := node.Registration{
		ID: node.ID(id), Type: typ, Operator: node.OperatorNetwork, RegionID: "region-se", Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}
	if typ.AllowsOperator(node.OperatorVendor) {
		in.Operator = node.OperatorVendor
		in.Vendor = &node.VendorRef{VendorID: "vendor-77", VendorNodeCode: "code-" + id}
	}
	n, _, err := node.Register(in, se(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	if typ == node.FulfillmentCenter {
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

func newLane() NewLane {
	return NewLane{ID: "lane-5c21d8", Mode: ModeGround, NominalTransitMinutes: 180}
}

func TestAllowedPairsPinTheModel(t *testing.T) {
	// model.yaml lane_rules.allowed, verbatim
	want := []Pair{
		{node.FulfillmentCenter, node.SortationCenter},
		{node.FulfillmentCenter, node.DeliveryStation},
		{node.FulfillmentCenter, node.AirHub},
		{node.SortationCenter, node.SortationCenter},
		{node.SortationCenter, node.DeliveryStation},
		{node.SortationCenter, node.AirHub},
		{node.AirHub, node.AirHub},
		{node.AirHub, node.SortationCenter},
		{node.AirHub, node.DeliveryStation},
		{node.InboundCrossDock, node.FulfillmentCenter},
	}
	got := AllowedPairs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllowedPairs() = %v", got)
	}
	got[0] = Pair{}
	if AllowedPairs()[0] != want[0] {
		t.Fatal("AllowedPairs() must return a copy")
	}
	allowed := 0
	for _, o := range node.Types() {
		for _, d := range node.Types() {
			isWant := false
			for _, w := range want {
				isWant = isWant || w == (Pair{o, d})
			}
			if PairAllowed(o, d) != isWant {
				t.Errorf("PairAllowed(%s, %s) = %v, want %v", o, d, !isWant, isWant)
			}
			if isWant {
				allowed++
			}
		}
	}
	if allowed != 10 {
		t.Fatalf("the model allows 10 pairs, found %d", allowed)
	}
}

func TestOpenEveryAllowedPair(t *testing.T) {
	for _, p := range AllowedPairs() {
		for _, mode := range []Mode{ModeGround, ModeAir} {
			o, d := mkNode(t, "node-a", p.Origin, node.StatusActive), mkNode(t, "node-b", p.Destination, node.StatusActive)
			in := newLane()
			in.Mode = mode
			l, events, err := Open(in, o, d, t1)
			if err != nil {
				t.Fatalf("%s to %s %s: %v", p.Origin, p.Destination, mode, err)
			}
			if l.Status() != StatusOpen || l.Version() != 1 || l.Mode() != mode {
				t.Fatalf("unexpected lane: %+v", l.Snapshot())
			}
			if len(events) != 1 {
				t.Fatalf("want one event, got %d", len(events))
			}
		}
	}
}

func TestOpenHappyPath(t *testing.T) {
	o, d := mkNode(t, "node-0f3a91", node.FulfillmentCenter, node.StatusActive), mkNode(t, "node-9b7e02", node.DeliveryStation, node.StatusActive)
	brt := time.FixedZone("BRT", -3*3600)
	l, events, err := Open(newLane(), o, d, t1.In(brt))
	if err != nil {
		t.Fatal(err)
	}
	assertOpenedLane(t, l)
	assertLaneOpenedEvent(t, events)
}

func assertOpenedLane(t *testing.T, l *Lane) {
	t.Helper()
	if l.ID() != "lane-5c21d8" || l.OriginNodeID() != "node-0f3a91" || l.DestinationNodeID() != "node-9b7e02" || l.Mode() != ModeGround ||
		l.NominalTransitMinutes() != 180 || l.Status() != StatusOpen || !l.OpenedAt().Equal(t1) || !l.ClosedAt().IsZero() || l.Version() != 1 {
		t.Fatalf("unexpected lane: %+v", l.Snapshot())
	}
}

func assertLaneOpenedEvent(t *testing.T, events []shared.Event) {
	t.Helper()
	want := LaneOpened{
		Header: shared.Header{Version: 1, At: t1}, LaneID: "lane-5c21d8", OriginNodeID: "node-0f3a91",
		DestinationNodeID: "node-9b7e02", Mode: ModeGround, NominalTransitMinutes: 180,
	}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	ev := events[0]
	if ev.EventName() != "LaneOpened" || ev.Entity() != "lane" || ev.SubjectID() != "lane-5c21d8" || ev.AggregateVersion() != 1 ||
		!ev.OccurredAt().Equal(t1) || ev.OccurredAt().Location() != time.UTC {
		t.Fatalf("accessors wrong: %+v", ev)
	}
}

func TestOpenRejections(t *testing.T) {
	fc := func(t *testing.T) *node.Node { return mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusActive) }
	sc := func(t *testing.T) *node.Node { return mkNode(t, "node-sc", node.SortationCenter, node.StatusActive) }
	tests := []struct {
		name string
		in   func() NewLane
		o, d func(*testing.T) *node.Node
		want error
	}{
		{"empty id", func() NewLane { n := newLane(); n.ID = ""; return n }, fc, sc, shared.ErrValidation},
		{"bad id", func() NewLane { n := newLane(); n.ID = "lane 1"; return n }, fc, sc, shared.ErrValidation},
		{"unknown mode", func() NewLane { n := newLane(); n.Mode = "RAIL"; return n }, fc, sc, shared.ErrValidation},
		{"empty mode", func() NewLane { n := newLane(); n.Mode = ""; return n }, fc, sc, shared.ErrValidation},
		{"transit zero", func() NewLane { n := newLane(); n.NominalTransitMinutes = 0; return n }, fc, sc, shared.ErrValidation},
		{"transit negative", func() NewLane { n := newLane(); n.NominalTransitMinutes = -5; return n }, fc, sc, shared.ErrValidation},
		{"transit above int32", func() NewLane { n := newLane(); n.NominalTransitMinutes = MaxTransitMinutes + 1; return n }, fc, sc, shared.ErrValidation},
		{"origin not loaded", newLane, func(*testing.T) *node.Node { return nil }, sc, ErrLaneEndpointNotActive},
		{"destination not loaded", newLane, fc, func(*testing.T) *node.Node { return nil }, ErrLaneEndpointNotActive},
		{"loop", newLane, sc, sc, ErrLaneLoop},
		{"pair not allowed", newLane, func(t *testing.T) *node.Node { return mkNode(t, "node-ds", node.DeliveryStation, node.StatusActive) }, fc, ErrLaneTypePairNotAllowed},
		{"proposed origin", newLane, func(t *testing.T) *node.Node {
			return mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusProposed)
		}, sc, ErrLaneEndpointNotActive},
		{"suspended destination", newLane, fc, func(t *testing.T) *node.Node { return mkNode(t, "node-sc", node.SortationCenter, node.StatusSuspended) }, ErrLaneEndpointNotActive},
		{"retired origin", newLane, func(t *testing.T) *node.Node { return mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusRetired) }, sc, ErrLaneEndpointNotActive},
		{"proposed destination", newLane, fc, func(t *testing.T) *node.Node { return mkNode(t, "node-sc", node.SortationCenter, node.StatusProposed) }, ErrLaneEndpointNotActive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, events, err := Open(tc.in(), tc.o(t), tc.d(t), t1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if l != nil || events != nil {
				t.Fatal("nothing may be returned on error")
			}
		})
	}
}

func TestOpenCheckOrder(t *testing.T) {
	ds := mkNode(t, "node-ds", node.DeliveryStation, node.StatusProposed)
	fc := mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusProposed)
	// a loop on a pair that is not allowed and not active is a loop
	if _, _, err := Open(newLane(), ds, ds, t1); !errors.Is(err, ErrLaneLoop) {
		t.Fatalf("loop first: %v", err)
	}
	// a disallowed pair beats inactive ends
	if _, _, err := Open(newLane(), ds, fc, t1); !errors.Is(err, ErrLaneTypePairNotAllowed) {
		t.Fatalf("pair before activity: %v", err)
	}
	// values beat everything
	bad := newLane()
	bad.Mode = "x"
	if _, _, err := Open(bad, nil, nil, t1); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("values first: %v", err)
	}
}

func TestOpenBoundaryValues(t *testing.T) {
	o, d := mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusActive), mkNode(t, "node-sc", node.SortationCenter, node.StatusActive)
	for _, minutes := range []int{1, MaxTransitMinutes} {
		in := newLane()
		in.NominalTransitMinutes = minutes
		if _, _, err := Open(in, o, d, t1); err != nil {
			t.Errorf("%d minutes must be accepted: %v", minutes, err)
		}
	}
}

func openLane(t *testing.T) *Lane {
	t.Helper()
	l, _, err := Open(newLane(), mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusActive), mkNode(t, "node-sc", node.SortationCenter, node.StatusActive), t1)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCloseTransitions(t *testing.T) {
	l := openLane(t)
	events, err := l.Close(t2.In(time.FixedZone("BRT", -3*3600)))
	if err != nil {
		t.Fatal(err)
	}
	want := LaneClosed{Header: shared.Header{Version: 2, At: t2}, LaneID: "lane-5c21d8", ClosedAt: t2}
	if !reflect.DeepEqual(events, []shared.Event{want}) {
		t.Fatalf("events = %+v", events)
	}
	ev := events[0]
	if ev.EventName() != "LaneClosed" || ev.Entity() != "lane" || ev.SubjectID() != "lane-5c21d8" || ev.AggregateVersion() != 2 || !ev.OccurredAt().Equal(t2) {
		t.Fatalf("accessors wrong: %+v", ev)
	}
	if l.Status() != StatusClosed || !l.ClosedAt().Equal(t2) || l.ClosedAt().Location() != time.UTC || l.Version() != 2 {
		t.Fatalf("unexpected lane: %+v", l.Snapshot())
	}
	// no reopen, no second close
	before := l.Snapshot()
	events, err = l.Close(t2.Add(time.Hour))
	if !errors.Is(err, shared.ErrIllegalTransition) || events != nil || !reflect.DeepEqual(l.Snapshot(), before) {
		t.Fatalf("a CLOSED lane cannot close again: %v", err)
	}
}

func TestCloseDoesNotNeedActiveEndpoints(t *testing.T) {
	o, d := mkNode(t, "node-fc", node.FulfillmentCenter, node.StatusActive), mkNode(t, "node-sc", node.SortationCenter, node.StatusActive)
	l, _, err := Open(newLane(), o, d, t1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Retire(t1); err != nil { // the lane stays OPEN (SuspendedEndpointsUnusable)
		t.Fatal(err)
	}
	if l.Status() != StatusOpen {
		t.Fatal("retiring an endpoint closes nothing")
	}
	if _, err := l.Close(t2); err != nil {
		t.Fatalf("closing needs no ACTIVE endpoint: %v", err)
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	open := openLane(t)
	closed := openLane(t)
	if _, err := closed.Close(t2); err != nil {
		t.Fatal(err)
	}
	for _, l := range []*Lane{open, closed} {
		snap := l.Snapshot()
		back, err := Restore(snap)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(back.Snapshot(), snap) {
			t.Fatalf("round trip changed the lane: %+v vs %+v", back.Snapshot(), snap)
		}
	}
	brt := time.FixedZone("BRT", -3*3600)
	s := open.Snapshot()
	s.OpenedAt = s.OpenedAt.In(brt)
	back, err := Restore(s)
	if err != nil || back.OpenedAt().Location() != time.UTC {
		t.Fatalf("restored instants are UTC: %v", err)
	}
}

func TestRestoreRejectsCorruptRows(t *testing.T) {
	closed := openLane(t)
	if _, err := closed.Close(t2); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		base Snapshot
		mut  func(*Snapshot)
	}{
		{"bad id", openLane(t).Snapshot(), func(s *Snapshot) { s.ID = "" }},
		{"bad origin", openLane(t).Snapshot(), func(s *Snapshot) { s.OriginNodeID = "" }},
		{"bad destination", openLane(t).Snapshot(), func(s *Snapshot) { s.DestinationNodeID = "a b" }},
		{"loop", openLane(t).Snapshot(), func(s *Snapshot) { s.DestinationNodeID = s.OriginNodeID }},
		{"unknown mode", openLane(t).Snapshot(), func(s *Snapshot) { s.Mode = "RAIL" }},
		{"transit zero", openLane(t).Snapshot(), func(s *Snapshot) { s.NominalTransitMinutes = 0 }},
		{"transit too large", openLane(t).Snapshot(), func(s *Snapshot) { s.NominalTransitMinutes = MaxTransitMinutes + 1 }},
		{"no openedAt", openLane(t).Snapshot(), func(s *Snapshot) { s.OpenedAt = time.Time{} }},
		{"version zero", openLane(t).Snapshot(), func(s *Snapshot) { s.Version = 0 }},
		{"unknown status", openLane(t).Snapshot(), func(s *Snapshot) { s.Status = "PAUSED" }},
		{"open with closedAt", openLane(t).Snapshot(), func(s *Snapshot) { s.ClosedAt = t2 }},
		{"closed without closedAt", closed.Snapshot(), func(s *Snapshot) { s.ClosedAt = time.Time{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.base
			tc.mut(&s)
			l, err := Restore(s)
			if !errors.Is(err, shared.ErrInvalidSnapshot) || l != nil {
				t.Fatalf("want ErrInvalidSnapshot, got %v", err)
			}
		})
	}
	if !strings.Contains(ErrDuplicateLane.Error(), "duplicate") || errors.Is(ErrDuplicateLane, ErrLaneLoop) {
		t.Fatal("ErrDuplicateLane is its own sentinel")
	}
	if !Mode("GROUND").Valid() || !Mode("AIR").Valid() || Mode("").Valid() {
		t.Fatal("Mode.Valid")
	}
}

func TestRestoreAcceptsTheTransitRangeEdges(t *testing.T) {
	for _, minutes := range []int{1, MaxTransitMinutes} {
		s := openLane(t).Snapshot()
		s.NominalTransitMinutes = minutes
		l, err := Restore(s)
		if err != nil || l.NominalTransitMinutes() != minutes {
			t.Fatalf("transit %d must restore: %v", minutes, err)
		}
	}
	s := openLane(t).Snapshot()
	s.NominalTransitMinutes = 0
	if _, err := Restore(s); err == nil {
		t.Fatal("transit 0 must be refused")
	}
}
