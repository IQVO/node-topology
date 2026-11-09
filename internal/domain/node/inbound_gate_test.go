package node

import (
	"errors"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

// receivingTypes are the node types with the INBOUND capability (ADR 0015).
var receivingTypes = []Type{FulfillmentCenter, InboundCrossDock}

func TestInboundCapabilityByType(t *testing.T) {
	for _, typ := range Types() {
		want := typ == FulfillmentCenter || typ == InboundCrossDock
		if got := typ.Capabilities().Inbound; got != want {
			t.Errorf("%s inbound = %v, want %v", typ, got, want)
		}
	}
	fc := FulfillmentCenter.Capabilities()
	if want := (Capabilities{Outbound: true, Inbound: true}); fc != want {
		t.Fatalf("FULFILLMENT_CENTER capabilities = %+v, want %+v", fc, want)
	}
	if n := nodeIn(t, FulfillmentCenter, StatusActive, false); !n.Capabilities().Inbound || !n.AcceptsDemand() {
		t.Fatal("an ACTIVE fulfillment centre receives inbound and still takes demand")
	}
}

// declarationFor returns a valid next-revision declaration for n carrying in.
func declarationFor(n *Node, in *InboundLimits) ProfileDeclaration {
	return decl(func(d *ProfileDeclaration) {
		d.Revision, d.Inbound = n.ProfileRevision()+1, in
		if n.Operator() == OperatorNetwork {
			d.DeclaredBy = DeclaredByNetworkOps
		}
		if n.Type() == SortationCenter || n.Type() == AirHub {
			d.SortDwellMinutes = intp(15)
		}
	})
}

func TestInboundLimitsAcceptedOnReceivingTypes(t *testing.T) {
	for _, typ := range receivingTypes {
		n := nodeIn(t, typ, StatusProposed, false)
		events, err := n.DeclareProfile(declarationFor(n, fullInbound()), t1)
		if err != nil {
			t.Fatalf("%s declares inbound limits: %v", typ, err)
		}
		if !reflect.DeepEqual(events[0].(NodeOperationalProfileDeclared).Inbound, fullInbound()) {
			t.Fatalf("%s: event inbound = %+v", typ, events[0].(NodeOperationalProfileDeclared).Inbound)
		}
	}
}

func TestInboundLimitsRefusedWithoutInboundCapability(t *testing.T) {
	for _, typ := range []Type{ReturnsCenter, SortationCenter, DeliveryStation, AirHub} {
		n := nodeIn(t, typ, StatusProposed, false)
		before := n.Snapshot()
		events, err := n.DeclareProfile(declarationFor(n, fullInbound()), t1)
		if !errors.Is(err, ErrInboundLimitsNotAllowed) || events != nil {
			t.Errorf("%s: want ErrInboundLimitsNotAllowed, got %v", typ, err)
		}
		if !reflect.DeepEqual(n.Snapshot(), before) {
			t.Errorf("%s: a refused declaration changes nothing", typ)
		}
		if _, err := n.DeclareProfile(declarationFor(n, nil), t1); err != nil {
			t.Errorf("%s: a profile without inbound stays accepted: %v", typ, err)
		}
	}
}

func TestInboundGateRunsBeforeTheValueChecks(t *testing.T) {
	// a node type without INBOUND learns that first, whatever the values
	sc := nodeIn(t, SortationCenter, StatusProposed, false)
	if _, err := sc.DeclareProfile(declarationFor(sc, &InboundLimits{}), t1); !errors.Is(err, ErrInboundLimitsNotAllowed) {
		t.Fatalf("empty inbound on a sortation centre: %v", err)
	}
	if _, err := sc.DeclareProfile(declarationFor(sc, &InboundLimits{MaxCartonWeightG: intp(0)}), t1); !errors.Is(err, ErrInboundLimitsNotAllowed) || errors.Is(err, shared.ErrValidation) {
		t.Fatalf("the gate must win over ErrValidation: %v", err)
	}
	// the value checks of ADR 0007 still apply when the type has INBOUND
	for _, typ := range receivingTypes {
		n := nodeIn(t, typ, StatusProposed, false)
		_, err := n.DeclareProfile(declarationFor(n, &InboundLimits{MaxCartonWeightG: intp(0)}), t1)
		if !errors.Is(err, shared.ErrValidation) || errors.Is(err, ErrInboundLimitsNotAllowed) {
			t.Errorf("%s: want ErrValidation, got %v", typ, err)
		}
	}
	// a stale revision is still reported before the gate
	if _, err := sc.DeclareProfile(decl(func(d *ProfileDeclaration) { d.Revision, d.Inbound = 0, fullInbound() }), t1); !errors.Is(err, ErrStaleProfileRevision) {
		t.Fatalf("stale before the gate: %v", err)
	}
}

func TestRestoreKeepsStoredInboundLimitsOnAnyType(t *testing.T) {
	// a revision stored before ADR 0015 may carry limits on a type without
	// INBOUND: Restore accepts it, only new declarations are refused
	for _, typ := range []Type{ReturnsCenter, SortationCenter, DeliveryStation, AirHub} {
		n := nodeIn(t, typ, StatusProposed, false)
		if _, err := n.DeclareProfile(declarationFor(n, nil), t1); err != nil {
			t.Fatal(err)
		}
		s := n.Snapshot()
		s.Profile.Inbound = fullInbound()
		back, err := Restore(s)
		if err != nil {
			t.Fatalf("%s: a stored profile with inbound limits is restorable: %v", typ, err)
		}
		if p, _ := back.Profile(); !reflect.DeepEqual(p.Inbound, fullInbound()) {
			t.Fatalf("%s: restored inbound = %+v", typ, p.Inbound)
		}
		if _, err := back.DeclareProfile(declarationFor(back, fullInbound()), t2); !errors.Is(err, ErrInboundLimitsNotAllowed) {
			t.Fatalf("%s: the next declaration is still gated: %v", typ, err)
		}
	}
}
