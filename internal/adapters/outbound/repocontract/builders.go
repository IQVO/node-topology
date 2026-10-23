package repocontract

import (
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// Now is the instant every builder stamps its aggregates with.
var Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// NewRegion defines a region.
func NewRegion(t testing.TB, id region.ID, code string) *region.Region {
	t.Helper()
	r, _, err := region.Define(region.NewRegion{ID: id, Code: code, Name: "Region " + code}, Now)
	if err != nil {
		t.Fatalf("define region %s: %v", id, err)
	}
	return r
}

// Declaration returns a valid profile declaration of the revision: Monday
// 06:00-22:00, a 14:00 Monday cutoff, 30 minutes of handling.
func Declaration(revision int) node.ProfileDeclaration {
	return node.ProfileDeclaration{
		Revision:            revision,
		Timezone:            "America/Sao_Paulo",
		OperatingHours:      node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
		HandlingTimeMinutes: 30,
		Cutoffs:             []node.CutoffSlot{{LocalTime: "14:00", Days: []node.Weekday{node.Monday}, MaxOrders: 10}},
		ThroughputCap:       node.ThroughputCap{Value: 100, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
		DeclaredBy:          node.DeclaredByNetworkOps,
	}
}

// NewNode registers a PROPOSED node of the type. A node type that a vendor may
// run gets operator VENDOR and the vendor reference (vendor-1, code); a parcel
// node is NETWORK-operated and ignores code.
func NewNode(t testing.TB, reg *region.Region, typ node.Type, id node.ID, code string) *node.Node {
	t.Helper()
	in := node.Registration{
		ID: id, Type: typ, Operator: node.OperatorNetwork, RegionID: reg.ID(),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	}
	if typ.AllowsOperator(node.OperatorVendor) {
		in.Operator = node.OperatorVendor
		in.Vendor = &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: code}
	}
	n, _, err := node.Register(in, reg, Now)
	if err != nil {
		t.Fatalf("register node %s: %v", id, err)
	}
	return n
}

// NewActiveNode registers a node and activates it (declaring a complete
// profile first, which a FULFILLMENT_CENTER needs).
func NewActiveNode(t testing.TB, reg *region.Region, typ node.Type, id node.ID, code string) *node.Node {
	t.Helper()
	n := NewNode(t, reg, typ, id, code)
	if _, err := n.DeclareProfile(Declaration(1), Now); err != nil {
		t.Fatalf("declare profile of %s: %v", id, err)
	}
	if _, err := n.Activate(Now); err != nil {
		t.Fatalf("activate %s: %v", id, err)
	}
	return n
}

// NewLane opens a GROUND lane between two ACTIVE nodes whose types are an
// allowed pair.
func NewLane(t testing.TB, id lane.ID, origin, destination *node.Node, mode lane.Mode) *lane.Lane {
	t.Helper()
	l, _, err := lane.Open(lane.NewLane{ID: id, Mode: mode, NominalTransitMinutes: 120}, origin, destination, Now)
	if err != nil {
		t.Fatalf("open lane %s: %v", id, err)
	}
	return l
}

// NewServiceArea assigns a prefix to an ACTIVE delivery station.
func NewServiceArea(t testing.TB, prefix servicearea.PostalPrefix, station *node.Node, reg *region.Region, version int) *servicearea.ServiceArea {
	t.Helper()
	a, _, err := servicearea.Assign(prefix, version, station, reg, Now)
	if err != nil {
		t.Fatalf("assign %s: %v", prefix, err)
	}
	return a
}

// AllowedPair returns the node types of the first allowed lane pair.
func AllowedPair(t testing.TB) (origin, destination node.Type) {
	t.Helper()
	pairs := lane.AllowedPairs()
	if len(pairs) == 0 {
		t.Fatal("the model allows no lane pair")
	}
	return pairs[0].Origin, pairs[0].Destination
}
