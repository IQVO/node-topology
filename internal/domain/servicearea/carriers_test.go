package servicearea

import (
	"errors"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/node"
)

// handoffDelivery builds a CARRIER_HANDOFF CarrierDelivery whose options hand
// over at the given (already built) nodes.
func handoffDelivery(ids ...node.ID) CarrierDelivery {
	d := CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: make([]CarrierOption, len(ids))}
	for i, id := range ids {
		d.Carriers[i] = CarrierOption{
			CarrierID: string(rune('a'+i)) + "-express", HandoffNodeID: id,
			CostPerPackageCents: int64(300 + 100*i), TransitMinutes: 60 + 30*i,
		}
	}
	return d
}

// withHandoffs attaches the loaded nodes to the delivery.
func withHandoffs(d CarrierDelivery, nodes ...*node.Node) CarrierDelivery {
	d.Handoffs = nodes
	return d
}

func TestCarrierHandoffAssignmentRoundTrip(t *testing.T) {
	st := station(t)
	sc := mkNode(t, "node-sc", node.SortationCenter, "region-se", node.StatusActive)
	d := withHandoffs(handoffDelivery(st.ID(), sc.ID()), st, sc)
	sa, events, err := Assign("01310", 1, st, mkRegion(t, "region-se"), d, t0)
	if err != nil {
		t.Fatal(err)
	}
	if sa.DeliveryMode() != ModeCarrierHandoff || len(sa.Carriers()) != 2 {
		t.Fatalf("mode=%s carriers=%d", sa.DeliveryMode(), len(sa.Carriers()))
	}
	ev, ok := events[0].(ServiceAreaAssigned)
	if !ok || ev.DeliveryMode != ModeCarrierHandoff || len(ev.Carriers) != 2 || ev.Carriers[0].CarrierID != "a-express" {
		t.Fatalf("event = %+v", events[0])
	}
	back, err := Restore(sa.Snapshot())
	if err != nil || back.DeliveryMode() != ModeCarrierHandoff || len(back.Carriers()) != 2 {
		t.Fatalf("round trip: %v %+v", err, back)
	}
}

func TestModeFlipsOnReassignment(t *testing.T) {
	st := station(t)
	sc := mkNode(t, "node-sc", node.SortationCenter, "region-se", node.StatusActive)
	sa := firstAssignment(t)
	if _, err := sa.Reassign(2, st, mkRegion(t, "region-se"), withHandoffs(handoffDelivery(sc.ID()), sc), t1); err != nil {
		t.Fatal(err)
	}
	if sa.DeliveryMode() != ModeCarrierHandoff || len(sa.Carriers()) != 1 {
		t.Fatalf("carrier reassignment: %s %+v", sa.DeliveryMode(), sa.Carriers())
	}
	if _, err := sa.Reassign(3, st, mkRegion(t, "region-se"), CarrierDelivery{}, t2); err != nil {
		t.Fatal(err)
	}
	if sa.DeliveryMode() != ModeNetworkRoute || sa.Carriers() != nil {
		t.Fatalf("route reassignment: %s %+v", sa.DeliveryMode(), sa.Carriers())
	}
}

func TestCarrierOptionListShape(t *testing.T) {
	st := station(t)
	sc := mkNode(t, "node-sc", node.SortationCenter, "region-se", node.StatusActive)
	reg := mkRegion(t, "region-se")
	cases := []struct {
		name string
		in   CarrierDelivery
		want error
	}{
		{"empty list on CARRIER_HANDOFF", CarrierDelivery{Mode: ModeCarrierHandoff}, ErrCarrierOptionsInvalid},
		{"five options", func() CarrierDelivery {
			d := CarrierDelivery{Mode: ModeCarrierHandoff}
			for i := 0; i < MaxCarriersPerArea+1; i++ {
				d.Carriers = append(d.Carriers, CarrierOption{
					CarrierID: string(rune('a'+i)) + "-x", HandoffNodeID: sc.ID(), TransitMinutes: 60,
				})
			}
			return withHandoffs(d, sc)
		}(), ErrCarrierOptionsInvalid},
		{"duplicate carrierId", func() CarrierDelivery {
			twice := handoffDelivery(sc.ID(), sc.ID())
			twice.Carriers[1].CarrierID = twice.Carriers[0].CarrierID
			return withHandoffs(twice, sc)
		}(), ErrCarrierOptionsInvalid},
		{"options on NETWORK_ROUTE", func() CarrierDelivery {
			d := withHandoffs(handoffDelivery(sc.ID()), sc)
			d.Mode = ModeNetworkRoute
			return d
		}(), ErrCarrierOptionsInvalid},
		{"unknown mode", CarrierDelivery{Mode: "TELEPORT"}, ErrCarrierOptionsInvalid},
		{"blank carrierId", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "  ", HandoffNodeID: sc.ID(), TransitMinutes: 60}}}, sc), ErrCarrierOptionsInvalid},
		{"malformed handoffNodeId", CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: "_bad", TransitMinutes: 60}}}, ErrCarrierOptionsInvalid},
		{"cost above the ceiling", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: sc.ID(), CostPerPackageCents: MaxCarrierCostCents + 1, TransitMinutes: 60}}}, sc), ErrCarrierOptionsInvalid},
		{"negative cost", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: sc.ID(), CostPerPackageCents: -1, TransitMinutes: 60}}}, sc), ErrCarrierOptionsInvalid},
		{"zero transit", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: sc.ID()}}}, sc), ErrCarrierOptionsInvalid},
		{"transit above the ceiling", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: sc.ID(), TransitMinutes: 2147483648}}}, sc), ErrCarrierOptionsInvalid},
		{"cost at the ceiling is legal", withHandoffs(CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{{CarrierID: "a-x", HandoffNodeID: sc.ID(), CostPerPackageCents: MaxCarrierCostCents, TransitMinutes: 2147483647}}}, sc), nil},
	}
	for _, tc := range cases { // ordered cases, one builder per row
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Assign("01310", 1, st, reg, tc.in, t0)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("refused a legal delivery: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHandoffNodeMustBeLoadedActiveAndCollectable(t *testing.T) {
	st := station(t)
	sc := mkNode(t, "node-sc", node.SortationCenter, "region-se", node.StatusActive)
	ds := mkNode(t, "node-ds2", node.DeliveryStation, "region-se", node.StatusActive)
	fc := mkNode(t, "node-fc", node.FulfillmentCenter, "region-se", node.StatusActive)
	proposed := mkNode(t, "node-new", node.SortationCenter, "region-se", node.StatusProposed)
	reg := mkRegion(t, "region-se")
	cases := []struct {
		name    string
		handoff node.ID
		loaded  []*node.Node
		want    error
	}{
		{"an ACTIVE delivery station", st.ID(), []*node.Node{st}, nil},
		{"an ACTIVE sortation centre", sc.ID(), []*node.Node{sc}, nil},
		{"two options share one loaded list", st.ID(), []*node.Node{st, sc, ds}, nil},
		{"not loaded", sc.ID(), nil, ErrHandoffNodeNotUsable},
		{"a FULFILLMENT_CENTER", fc.ID(), []*node.Node{fc}, ErrHandoffNodeNotUsable},
		{"a PROPOSED node", proposed.ID(), []*node.Node{proposed}, ErrHandoffNodeNotUsable},
	}
	for _, tc := range cases { // ordered cases, one builder per row
		t.Run(tc.name, func(t *testing.T) {
			d := CarrierDelivery{Mode: ModeCarrierHandoff, Carriers: []CarrierOption{
				{CarrierID: "a-express", HandoffNodeID: tc.handoff, CostPerPackageCents: 320, TransitMinutes: 95},
			}, Handoffs: tc.loaded}
			_, _, err := Assign("01310", 1, st, reg, d, t0)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("refused a usable handoff node: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCarriersAccessorCopies(t *testing.T) {
	st := station(t)
	sa, _, err := Assign("01310", 1, st, mkRegion(t, "region-se"), withHandoffs(handoffDelivery(st.ID()), st), t0)
	if err != nil {
		t.Fatal(err)
	}
	got := sa.Carriers()
	got[0].CarrierID = "mutated"
	if sa.Carriers()[0].CarrierID == "mutated" {
		t.Fatal("Carriers() aliased the aggregate's list")
	}
}
