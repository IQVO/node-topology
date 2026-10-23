package views_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// wire marshals v and decodes it into a generic map.
func wire(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func requireKeys(t *testing.T, m map[string]any, want ...string) {
	t.Helper()
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %q in %v", k, keys(m))
		}
	}
}

func forbidKeys(t *testing.T, m map[string]any, banned ...string) {
	t.Helper()
	for _, k := range banned {
		if _, ok := m[k]; ok {
			t.Fatalf("unexpected %q in %v", k, keys(m))
		}
	}
}

func activeFulfillmentCenter(t *testing.T) *node.Node {
	t.Helper()
	reg := repocontract.NewRegion(t, "r-1", "SP")
	n := repocontract.NewNode(t, reg, node.FulfillmentCenter, "n-1", "FC-1")
	dwell := 7
	d := repocontract.Declaration(1)
	d.SortDwellMinutes = &dwell
	d.Holidays = []string{"2026-12-25", "2026-01-01"}
	if _, err := n.DeclareProfile(d, repocontract.Now); err == nil {
		t.Fatal("a fulfillment centre must refuse a sort dwell")
	}
	d.SortDwellMinutes = nil
	for _, step := range []func() error{
		func() error { _, err := n.DeclareProfile(d, repocontract.Now); return err },
		func() error { _, err := n.Activate(repocontract.Now); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	return n
}

func TestNodeFromMapsEveryField(t *testing.T) {
	v := views.NodeFrom(activeFulfillmentCenter(t))

	assertNodeIdentity(t, v)
	assertVendorAndLocation(t, v)
	assertTimestamps(t, v)
	assertProfileScalars(t, v.OperationalProfile)
	assertProfileSchedule(t, v.OperationalProfile)
}

func assertNodeIdentity(t *testing.T, v views.Node) {
	t.Helper()
	if v.NodeID != "n-1" || v.NodeType != "FULFILLMENT_CENTER" || v.Operator != "VENDOR" || v.RegionID != "r-1" ||
		v.Timezone != "America/Sao_Paulo" || v.Status != "ACTIVE" {
		t.Fatalf("node = %+v", v)
	}
	if !v.Capabilities.Outbound || v.Capabilities.LastMile {
		t.Fatalf("capabilities = %+v", v.Capabilities)
	}
}

func assertVendorAndLocation(t *testing.T, v views.Node) {
	t.Helper()
	if v.VendorRef == nil || *v.VendorRef != (views.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-1"}) {
		t.Fatalf("vendorRef = %+v", v.VendorRef)
	}
	if v.Location.PostalCode != "01310" || v.Location.CountryCode != "BR" || v.Location.Lat != nil {
		t.Fatalf("location = %+v", v.Location)
	}
}

func assertTimestamps(t *testing.T, v views.Node) {
	t.Helper()
	if v.ActivatedAt == nil || !v.ActivatedAt.Equal(repocontract.Now) {
		t.Fatalf("activatedAt = %v", v.ActivatedAt)
	}
	if v.SuspendedAt != nil || v.RetiredAt != nil || v.SuspendReason != "" {
		t.Fatalf("a never-suspended, never-retired node carries suspension data: %+v", v)
	}
}

func assertProfileScalars(t *testing.T, p *views.OperationalProfile) {
	t.Helper()
	if p == nil || p.ProfileRevision != 1 || p.HandlingTimeMinutes != 30 || p.DeclaredBy != "NETWORK_OPS" || !p.DeclaredAt.Equal(repocontract.Now) {
		t.Fatalf("profile = %+v", p)
	}
	if p.ThroughputCap != (views.ThroughputCap{Value: 100, TimeUnit: "HOUR", Unit: "ORDER"}) || p.SortDwellMinutes != nil {
		t.Fatalf("cap = %+v dwell=%v", p.ThroughputCap, p.SortDwellMinutes)
	}
}

func assertProfileSchedule(t *testing.T, p *views.OperationalProfile) {
	t.Helper()
	if !reflect.DeepEqual(p.Holidays, []string{"2026-01-01", "2026-12-25"}) {
		t.Fatalf("holidays = %v", p.Holidays)
	}
	if p.OperatingHoursByDay["monday"] != (views.DayHours{Start: "06:00", End: "22:00"}) {
		t.Fatalf("hours = %+v", p.OperatingHoursByDay)
	}
	wantCutoff := views.CutoffSlot{LocalTime: "14:00", Days: []string{"monday"}, MaxOrders: 10}
	if !reflect.DeepEqual(p.Cutoffs, []views.CutoffSlot{wantCutoff}) {
		t.Fatalf("cutoffs = %+v", p.Cutoffs)
	}
}

func TestNodeFromCarriesSortDwellAndCoordinates(t *testing.T) {
	reg := repocontract.NewRegion(t, "r-1", "SP")
	lat, lon := -23.5, -46.6
	n, _, err := node.Register(node.Registration{
		ID: "n-2", Type: node.SortationCenter, Operator: node.OperatorNetwork, RegionID: reg.ID(), Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR", Lat: &lat, Lon: &lon},
	}, reg, repocontract.Now)
	if err != nil {
		t.Fatal(err)
	}
	dwell := 15
	d := repocontract.Declaration(1)
	d.SortDwellMinutes = &dwell
	if _, err := n.DeclareProfile(d, repocontract.Now); err != nil {
		t.Fatal(err)
	}

	v := views.NodeFrom(n)

	if v.Location.Lat == nil || *v.Location.Lat != lat || *v.Location.Lon != lon {
		t.Fatalf("location = %+v", v.Location)
	}
	if v.OperationalProfile.SortDwellMinutes == nil || *v.OperationalProfile.SortDwellMinutes != 15 {
		t.Fatalf("profile = %+v", v.OperationalProfile)
	}
	dwell = 99
	if *v.OperationalProfile.SortDwellMinutes != 15 {
		t.Fatal("the view aliases a caller's value")
	}
}

func TestNodeFromSuspendedAndRetired(t *testing.T) {
	n := activeFulfillmentCenter(t)
	if _, err := n.Suspend("flood", repocontract.Now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	v := views.NodeFrom(n)
	if v.SuspendReason != "flood" || v.SuspendedAt == nil || !v.SuspendedAt.Equal(repocontract.Now.Add(time.Hour)) {
		t.Fatalf("suspended view = %+v", v)
	}
	if _, err := n.Retire(repocontract.Now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	v = views.NodeFrom(n)
	if v.Status != "RETIRED" || v.RetiredAt == nil || !v.RetiredAt.Equal(repocontract.Now.Add(2*time.Hour)) {
		t.Fatalf("retired view = %+v", v)
	}
}

func TestNodeWireNamesFollowTheOpenAPI(t *testing.T) {
	m := wire(t, views.NodeFrom(activeFulfillmentCenter(t)))
	requireKeys(t, m, "nodeId", "nodeType", "operator", "vendorRef", "regionId", "timezone", "location",
		"capabilities", "status", "operationalProfile", "activatedAt")
	forbidKeys(t, m, "subtype", "suspendedAt", "suspendReason", "retiredAt")
	requireKeys(t, m["capabilities"].(map[string]any), "outbound", "inbound", "returns", "sort", "lastMile", "air")
	requireKeys(t, m["vendorRef"].(map[string]any), "vendorId", "vendorNodeCode")
	forbidKeys(t, m["location"].(map[string]any), "lat", "lon")
	p := m["operationalProfile"].(map[string]any)
	requireKeys(t, p, "profileRevision", "timezone", "operatingHoursByDay", "holidays", "handlingTimeMinutes",
		"cutoffs", "throughputCap", "declaredBy", "declaredAt")
	forbidKeys(t, p, "sortDwellMinutes")
	requireKeys(t, p["throughputCap"].(map[string]any), "value", "timeUnit", "unit")
	requireKeys(t, p["operatingHoursByDay"].(map[string]any)["monday"].(map[string]any), "start", "end")
}

func TestNodeWithoutProfileOrVendor(t *testing.T) {
	reg := repocontract.NewRegion(t, "r-1", "SP")
	v := views.NodeFrom(repocontract.NewNode(t, reg, node.DeliveryStation, "n-3", ""))
	if v.OperationalProfile != nil || v.VendorRef != nil || v.ActivatedAt != nil {
		t.Fatalf("view = %+v", v)
	}
	forbidKeys(t, wire(t, v), "operationalProfile", "vendorRef", "activatedAt")
}

func TestProfileFromACutoffWithoutDays(t *testing.T) {
	d := repocontract.Declaration(1)
	d.Cutoffs = []node.CutoffSlot{{LocalTime: "10:00"}}
	p, err := node.NewOperationalProfile(d, repocontract.Now)
	if err != nil {
		t.Fatal(err)
	}
	v := views.ProfileFrom(p)
	if len(v.Cutoffs) != 1 || v.Cutoffs[0].Days != nil || v.Cutoffs[0].MaxOrders != 0 {
		t.Fatalf("cutoffs = %+v", v.Cutoffs)
	}
	cut := wire(t, v)["cutoffs"].([]any)[0].(map[string]any)
	forbidKeys(t, cut, "days", "maxOrders")
}

func TestLaneFrom(t *testing.T) {
	reg := repocontract.NewRegion(t, "r-1", "SP")
	ot, dt := repocontract.AllowedPair(t)
	o := repocontract.NewActiveNode(t, reg, ot, "n-o", "FC-O")
	d := repocontract.NewActiveNode(t, reg, dt, "n-d", "FC-D")
	l := repocontract.NewLane(t, "lane-1", o, d, lane.ModeAir)

	v := views.LaneFrom(l)
	want := views.Lane{LaneID: "lane-1", OriginNodeID: "n-o", DestinationNodeID: "n-d", Mode: "AIR", NominalTransitMinutes: 120, Status: "OPEN"}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("lane = %+v, want %+v", v, want)
	}
	forbidKeys(t, wire(t, v), "closedAt")

	if _, err := l.Close(repocontract.Now); err != nil {
		t.Fatal(err)
	}
	v = views.LaneFrom(l)
	if v.Status != "CLOSED" || v.ClosedAt == nil {
		t.Fatalf("lane = %+v", v)
	}
	requireKeys(t, wire(t, v), "laneId", "originNodeId", "destinationNodeId", "mode", "nominalTransitMinutes", "status", "closedAt")
}

func TestServiceAreaAndRegionFrom(t *testing.T) {
	reg := repocontract.NewRegion(t, "r-1", "SP")
	st := repocontract.NewActiveNode(t, reg, node.DeliveryStation, "n-s", "")
	a := views.ServiceAreaFrom(repocontract.NewServiceArea(t, "013", st, reg, 3))
	if a != (views.ServiceArea{PostalPrefix: "013", DeliveryStationNodeID: "n-s", RegionID: "r-1", AssignmentVersion: 3}) {
		t.Fatalf("service area = %+v", a)
	}
	requireKeys(t, wire(t, a), "postalPrefix", "deliveryStationNodeId", "regionId", "assignmentVersion")

	r := views.RegionFrom(reg)
	if r != (views.Region{RegionID: "r-1", Code: "SP", Name: "Region SP"}) {
		t.Fatalf("region = %+v", r)
	}
	requireKeys(t, wire(t, r), "regionId", "code", "name")
}

func TestRegionsFromKeepsOrderAndIsNeverNil(t *testing.T) {
	if got := views.RegionsFrom(nil); got == nil || len(got) != 0 {
		t.Fatalf("RegionsFrom(nil) = %#v, want a non-nil empty slice", got)
	}
	sp, rj := repocontract.NewRegion(t, "r-1", "SP"), repocontract.NewRegion(t, "r-2", "RJ")
	got := views.RegionsFrom([]*region.Region{rj, sp})
	want := []views.Region{{RegionID: "r-2", Code: "RJ", Name: "Region RJ"}, {RegionID: "r-1", Code: "SP", Name: "Region SP"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("views = %+v, want %+v", got, want)
	}
}
