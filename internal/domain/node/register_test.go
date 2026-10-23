package node

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

func sudeste(t *testing.T) *region.Region {
	t.Helper()
	r, _, err := region.Define(region.NewRegion{ID: "region-se", Code: "SE", Name: "Southeast"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func f64(v float64) *float64 { return &v }

func location() GeoLocation {
	return GeoLocation{PostalCode: "07750-000", CountryCode: "BR", Lat: f64(-23.35), Lon: f64(-46.87)}
}

// vendorReg is a vendor-run SORTABLE fulfillment centre.
func vendorReg() Registration {
	return Registration{
		ID: "node-0f3a91", Type: FulfillmentCenter, Subtype: SubtypeSortable, Operator: OperatorVendor,
		Vendor:   &VendorRef{VendorID: "vendor-77", VendorNodeCode: "SP-CAJAMAR-01"},
		RegionID: "region-se", Timezone: "America/Sao_Paulo", Location: location(),
	}
}

func networkReg(typ Type) Registration {
	return Registration{ID: "node-net-1", Type: typ, Operator: OperatorNetwork, RegionID: "region-se", Timezone: "America/Sao_Paulo", Location: location()}
}

func reg(mut func(*Registration)) Registration {
	r := vendorReg()
	mut(&r)
	return r
}

func mustRegister(t *testing.T, in Registration) *Node {
	t.Helper()
	n, _, err := Register(in, sudeste(t), t0)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return n
}

func TestRegisterVendorFulfillmentCenter(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	n, events, err := Register(vendorReg(), sudeste(t), t0.In(brt))
	if err != nil {
		t.Fatal(err)
	}
	assertRegisteredVendorNode(t, n)
	assertNodeRegisteredEvent(t, events)
}

func assertRegisteredVendorNode(t *testing.T, n *Node) {
	t.Helper()
	if n.ID() != "node-0f3a91" || n.Type() != FulfillmentCenter || n.Subtype() != SubtypeSortable || n.Operator() != OperatorVendor ||
		n.RegionID() != "region-se" || n.Timezone() != "America/Sao_Paulo" || n.Status() != StatusProposed || n.Version() != 1 {
		t.Fatalf("unexpected node: %+v", n.Snapshot())
	}
	assertRegisteredVendorNodeDetails(t, n)
}

func assertRegisteredVendorNodeDetails(t *testing.T, n *Node) {
	t.Helper()
	if v, ok := n.Vendor(); !ok || v != (VendorRef{VendorID: "vendor-77", VendorNodeCode: "SP-CAJAMAR-01"}) {
		t.Fatalf("vendor = %+v %v", v, ok)
	}
	if !reflect.DeepEqual(n.Location(), location()) {
		t.Fatalf("location = %+v", n.Location())
	}
	if n.Capabilities() != (Capabilities{Outbound: true}) {
		t.Fatalf("capabilities must be derived from the type: %+v", n.Capabilities())
	}
	if _, ok := n.Profile(); ok || n.ProfileRevision() != 0 {
		t.Fatal("a new node has no profile")
	}
	if !n.ActivatedAt().IsZero() || !n.SuspendedAt().IsZero() || n.SuspendReason() != "" || !n.RetiredAt().IsZero() {
		t.Fatal("a PROPOSED node has no lifecycle timestamps")
	}
}

func assertNodeRegisteredEvent(t *testing.T, events []shared.Event) {
	t.Helper()
	want := NodeRegistered{
		Header: shared.Header{Version: 1, At: t0}, NodeID: "node-0f3a91", NodeType: FulfillmentCenter, Subtype: SubtypeSortable,
		Operator: OperatorVendor, VendorID: "vendor-77", VendorNodeCode: "SP-CAJAMAR-01", RegionID: "region-se",
		Timezone: "America/Sao_Paulo", PostalCode: "07750-000", CountryCode: "BR",
	}
	if len(events) != 1 || !reflect.DeepEqual(events[0], want) {
		t.Fatalf("events = %+v\nwant     %+v", events, want)
	}
	if events[0].OccurredAt().Location() != time.UTC {
		t.Fatal("event time must be UTC")
	}
	assertAccessors(t, events[0], "NodeRegistered", "node-0f3a91", 1, t0)
}

func assertAccessors(t *testing.T, ev shared.Event, name, subject string, version int64, at time.Time) {
	t.Helper()
	if ev.EventName() != name || ev.Entity() != "node" || ev.SubjectID() != subject || ev.AggregateVersion() != version || !ev.OccurredAt().Equal(at) {
		t.Fatalf("accessors of %T: %s %s %s %d %v", ev, ev.EventName(), ev.Entity(), ev.SubjectID(), ev.AggregateVersion(), ev.OccurredAt())
	}
}

func TestRegisterNetworkNodesAndVendorNodesOfEveryType(t *testing.T) {
	for _, row := range modelRows() {
		in := networkReg(row.typ)
		if row.operators[0] == OperatorVendor {
			in = Registration{
				ID: "node-v", Type: row.typ, Operator: OperatorVendor, Vendor: &VendorRef{VendorID: "v", VendorNodeCode: "c"},
				RegionID: "region-se", Timezone: "UTC", Location: GeoLocation{PostalCode: "1", CountryCode: "AA"},
			}
		}
		n, events, err := Register(in, sudeste(t), t0)
		if err != nil {
			t.Fatalf("%s: %v", row.typ, err)
		}
		if n.Capabilities() != row.capabilities {
			t.Errorf("%s: capabilities %+v, want %+v", row.typ, n.Capabilities(), row.capabilities)
		}
		ev := events[0].(NodeRegistered)
		if _, isVendor := n.Vendor(); isVendor != (row.operators[0] == OperatorVendor) || (ev.VendorID != "") != isVendor || (ev.VendorNodeCode != "") != isVendor {
			t.Errorf("%s: vendor ref and event disagree: %+v", row.typ, ev)
		}
	}
}

func TestRegisterOperatorMatrix(t *testing.T) {
	for _, row := range modelRows() {
		for _, o := range []Operator{OperatorNetwork, OperatorVendor} {
			in := Registration{ID: "n1", Type: row.typ, Operator: o, RegionID: "region-se", Timezone: "UTC", Location: GeoLocation{PostalCode: "1", CountryCode: "AA"}}
			if o == OperatorVendor {
				in.Vendor = &VendorRef{VendorID: "v", VendorNodeCode: "c"}
			}
			_, events, err := Register(in, sudeste(t), t0)
			if o == row.operators[0] {
				if err != nil {
					t.Errorf("%s by %s must be allowed: %v", row.typ, o, err)
				}
				continue
			}
			if !errors.Is(err, ErrOperatorNotAllowedForNodeType) || events != nil {
				t.Errorf("%s by %s: want ErrOperatorNotAllowedForNodeType, got %v", row.typ, o, err)
			}
		}
	}
}

func TestRegisterRejections(t *testing.T) {
	tests := []struct {
		name string
		in   Registration
		reg  func(*testing.T) *region.Region
		want error
	}{
		{"unknown type", reg(func(r *Registration) { r.Type = "WAREHOUSE" }), nil, shared.ErrValidation},
		{"empty type", reg(func(r *Registration) { r.Type = "" }), nil, shared.ErrValidation},
		{"unknown operator", reg(func(r *Registration) { r.Operator = "PARTNER" }), nil, shared.ErrValidation},
		{"empty id", reg(func(r *Registration) { r.ID = "" }), nil, shared.ErrValidation},
		{"id with space", reg(func(r *Registration) { r.ID = "node 1" }), nil, shared.ErrValidation},
		{"empty region id", reg(func(r *Registration) { r.RegionID = "" }), nil, shared.ErrValidation},
		{"vendor node without reference", reg(func(r *Registration) { r.Vendor = nil }), nil, shared.ErrValidation},
		{"vendor id blank", reg(func(r *Registration) { r.Vendor = &VendorRef{VendorID: " ", VendorNodeCode: "c"} }), nil, shared.ErrValidation},
		{"vendor node code empty", reg(func(r *Registration) { r.Vendor = &VendorRef{VendorID: "v"} }), nil, shared.ErrValidation},
		{"vendor id empty", reg(func(r *Registration) { r.Vendor = &VendorRef{VendorNodeCode: "c"} }), nil, shared.ErrValidation},
		{"vendor node code too long", reg(func(r *Registration) {
			r.Vendor = &VendorRef{VendorID: "v", VendorNodeCode: strings.Repeat("c", shared.MaxText+1)}
		}), nil, shared.ErrValidation},
		{"network node with a vendor reference", func() Registration {
			r := networkReg(SortationCenter)
			r.Vendor = &VendorRef{VendorID: "v", VendorNodeCode: "c"}
			return r
		}(), nil, shared.ErrValidation},
		{"bad timezone", reg(func(r *Registration) { r.Timezone = "Sao Paulo" }), nil, shared.ErrValidation},
		{"empty postal code", reg(func(r *Registration) { r.Location.PostalCode = "" }), nil, shared.ErrValidation},
		{"untrimmed postal code", reg(func(r *Registration) { r.Location.PostalCode = " 07750" }), nil, shared.ErrValidation},
		{"postal code too long", reg(func(r *Registration) { r.Location.PostalCode = strings.Repeat("1", MaxPostalCode+1) }), nil, shared.ErrValidation},
		{"lowercase country", reg(func(r *Registration) { r.Location.CountryCode = "br" }), nil, shared.ErrValidation},
		{"country of one letter", reg(func(r *Registration) { r.Location.CountryCode = "B" }), nil, shared.ErrValidation},
		{"country of three letters", reg(func(r *Registration) { r.Location.CountryCode = "BRA" }), nil, shared.ErrValidation},
		{"country below A", reg(func(r *Registration) { r.Location.CountryCode = "@B" }), nil, shared.ErrValidation},
		{"country above Z", reg(func(r *Registration) { r.Location.CountryCode = "A[" }), nil, shared.ErrValidation},
		{"country second below A", reg(func(r *Registration) { r.Location.CountryCode = "A@" }), nil, shared.ErrValidation},
		{"country first above Z", reg(func(r *Registration) { r.Location.CountryCode = "[A" }), nil, shared.ErrValidation},
		{"lat without lon", reg(func(r *Registration) { r.Location.Lon = nil }), nil, shared.ErrValidation},
		{"lon without lat", reg(func(r *Registration) { r.Location.Lat = nil }), nil, shared.ErrValidation},
		{"lat too north", reg(func(r *Registration) { r.Location.Lat = f64(90.01) }), nil, shared.ErrValidation},
		{"lat too south", reg(func(r *Registration) { r.Location.Lat = f64(-90.01) }), nil, shared.ErrValidation},
		{"lon too east", reg(func(r *Registration) { r.Location.Lon = f64(180.01) }), nil, shared.ErrValidation},
		{"lon too west", reg(func(r *Registration) { r.Location.Lon = f64(-180.01) }), nil, shared.ErrValidation},
		{"no region loaded", vendorReg(), func(*testing.T) *region.Region { return nil }, region.ErrUnknownRegion},
		{"another region loaded", reg(func(r *Registration) { r.RegionID = "region-ne" }), nil, region.ErrUnknownRegion},
		{"subtype on a cross dock", Registration{
			ID: "n1", Type: InboundCrossDock, Subtype: SubtypeSortable, Operator: OperatorVendor,
			Vendor: &VendorRef{VendorID: "v", VendorNodeCode: "c"}, RegionID: "region-se", Timezone: "UTC", Location: location(),
		}, nil, ErrSubtypeNotAllowed},
		{"subtype on a delivery station", func() Registration {
			r := networkReg(DeliveryStation)
			r.Subtype = SubtypeXL
			return r
		}(), nil, ErrSubtypeNotAllowed},
		{"unknown fulfillment subtype", reg(func(r *Registration) { r.Subtype = "HUGE" }), nil, ErrSubtypeNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := sudeste(t)
			if tc.reg != nil {
				r = tc.reg(t)
			}
			n, events, err := Register(tc.in, r, t0)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if n != nil || events != nil {
				t.Fatal("nothing may be returned on error")
			}
		})
	}
}

func TestRegisterCheckOrder(t *testing.T) {
	// operator beats every value check
	in := reg(func(r *Registration) { r.Operator, r.Vendor, r.ID, r.Timezone = OperatorNetwork, nil, "", "" })
	if _, _, err := Register(in, nil, t0); !errors.Is(err, ErrOperatorNotAllowedForNodeType) {
		t.Fatalf("operator first: %v", err)
	}
	// value checks beat the region
	in = reg(func(r *Registration) { r.Timezone = "" })
	if _, _, err := Register(in, nil, t0); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("values before region: %v", err)
	}
	// the region beats the subtype
	in = reg(func(r *Registration) { r.Subtype = "HUGE" })
	if _, _, err := Register(in, nil, t0); !errors.Is(err, region.ErrUnknownRegion) {
		t.Fatalf("region before subtype: %v", err)
	}
	// a fulfillment centre may omit the subtype
	if _, _, err := Register(reg(func(r *Registration) { r.Subtype = "" }), sudeste(t), t0); err != nil {
		t.Fatalf("subtype is optional: %v", err)
	}
}

func TestRegisterBoundaryValuesAreAccepted(t *testing.T) {
	cases := []func(*Registration){
		func(r *Registration) { r.Location.PostalCode = strings.Repeat("1", MaxPostalCode) },
		func(r *Registration) { r.Location.CountryCode = "AZ" },
		func(r *Registration) { r.Location.Lat, r.Location.Lon = f64(90), f64(180) },
		func(r *Registration) { r.Location.Lat, r.Location.Lon = f64(-90), f64(-180) },
		func(r *Registration) { r.Location.Lat, r.Location.Lon = nil, nil },
		func(r *Registration) {
			r.Vendor = &VendorRef{VendorID: strings.Repeat("v", shared.MaxText), VendorNodeCode: strings.Repeat("c", shared.MaxText)}
		},
		func(r *Registration) { r.ID = ID("n" + strings.Repeat("x", shared.MaxID-1)) },
	}
	for i, mut := range cases {
		if _, _, err := Register(reg(mut), sudeste(t), t0); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestRegisterCopiesItsInputs(t *testing.T) {
	in := vendorReg()
	n, _, err := Register(in, sudeste(t), t0)
	if err != nil {
		t.Fatal(err)
	}
	in.Vendor.VendorID = "changed"
	*in.Location.Lat = 0
	if v, _ := n.Vendor(); v.VendorID != "vendor-77" {
		t.Fatal("the node aliased the caller's vendor reference")
	}
	if *n.Location().Lat != -23.35 {
		t.Fatal("the node aliased the caller's coordinates")
	}
	got := n.Location()
	*got.Lat = 1
	if *n.Location().Lat != -23.35 {
		t.Fatal("Location() must return a copy")
	}
	s := n.Snapshot()
	s.Vendor.VendorID = "x"
	*s.Location.Lon = 1
	if v, _ := n.Vendor(); v.VendorID != "vendor-77" || *n.Location().Lon != -46.87 {
		t.Fatal("Snapshot() must return copies")
	}
}
