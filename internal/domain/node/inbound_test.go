package node

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/shared"
)

func boolp(v bool) *bool { return &v }

func fullInbound() *InboundLimits {
	return &InboundLimits{
		MaxCartonWeightG: intp(25000),
		MaxCartonDimsMm:  &CartonDims{L: 600, W: 400, H: 400},
		AcceptsPallets:   boolp(true),
		RequiresSscc:     boolp(false),
	}
}

func withInbound(l *InboundLimits) ProfileDeclaration {
	return decl(func(d *ProfileDeclaration) { d.Inbound = l })
}

func TestInboundLimitsAcceptedValues(t *testing.T) {
	tests := []struct {
		name string
		in   *InboundLimits
	}{
		{"none declared", nil},
		{"every field", fullInbound()},
		{"weight only", &InboundLimits{MaxCartonWeightG: intp(1)}},
		{"weight at the ceiling", &InboundLimits{MaxCartonWeightG: intp(MaxInboundLimit)}},
		{"smallest dims", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: 1, H: 1}}},
		{"dims at the ceiling", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: MaxInboundLimit, W: MaxInboundLimit, H: MaxInboundLimit}}},
		{"false flags are a declaration", &InboundLimits{AcceptsPallets: boolp(false)}},
		{"requiresSscc only", &InboundLimits{RequiresSscc: boolp(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewOperationalProfile(withInbound(tc.in), t0)
			if err != nil {
				t.Fatalf("want accepted, got %v", err)
			}
			if !reflect.DeepEqual(p.Inbound, tc.in) {
				t.Fatalf("inbound = %+v, want %+v", p.Inbound, tc.in)
			}
		})
	}
}

func TestInboundLimitsRefusedValues(t *testing.T) {
	over := MaxInboundLimit + 1
	tests := []struct {
		name string
		in   *InboundLimits
		want string
	}{
		{"declares nothing", &InboundLimits{}, "at least one limit"},
		{"weight zero", &InboundLimits{MaxCartonWeightG: intp(0)}, "maxCartonWeightG"},
		{"weight negative", &InboundLimits{MaxCartonWeightG: intp(-1)}, "maxCartonWeightG"},
		{"weight over the ceiling", &InboundLimits{MaxCartonWeightG: &over}, "maxCartonWeightG"},
		{"length zero", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 0, W: 1, H: 1}}, "maxCartonDimsMm.l"},
		{"width zero", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: 0, H: 1}}, "maxCartonDimsMm.w"},
		{"height zero", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: 1, H: 0}}, "maxCartonDimsMm.h"},
		{"length negative", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: -5, W: 1, H: 1}}, "maxCartonDimsMm.l"},
		{"width negative", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: -5, H: 1}}, "maxCartonDimsMm.w"},
		{"height negative", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: 1, H: -5}}, "maxCartonDimsMm.h"},
		{"length over the ceiling", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: over, W: 1, H: 1}}, "maxCartonDimsMm.l"},
		{"width over the ceiling", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: over, H: 1}}, "maxCartonDimsMm.w"},
		{"height over the ceiling", &InboundLimits{MaxCartonDimsMm: &CartonDims{L: 1, W: 1, H: over}}, "maxCartonDimsMm.h"},
		{"valid weight, bad dims", &InboundLimits{MaxCartonWeightG: intp(10), MaxCartonDimsMm: &CartonDims{}}, "maxCartonDimsMm"},
		{"bad weight, valid dims", &InboundLimits{MaxCartonWeightG: intp(0), MaxCartonDimsMm: &CartonDims{L: 1, W: 1, H: 1}}, "maxCartonWeightG"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOperationalProfile(withInbound(tc.in), t0)
			if !errors.Is(err, shared.ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want ErrValidation naming %q, got %v", tc.want, err)
			}
		})
	}
}

func TestInboundLimitsCloneIsDeep(t *testing.T) {
	if (*InboundLimits)(nil).clone() != nil {
		t.Fatal("cloning nil must give nil")
	}
	orig := fullInbound()
	c := orig.clone()
	if !reflect.DeepEqual(c, orig) {
		t.Fatalf("clone = %+v, want %+v", c, orig)
	}
	*c.MaxCartonWeightG = 1
	c.MaxCartonDimsMm.L = 1
	*c.AcceptsPallets = false
	*c.RequiresSscc = true
	if !reflect.DeepEqual(orig, fullInbound()) {
		t.Fatalf("mutating the clone changed the original: %+v", orig)
	}
}

func TestDeclareProfileCarriesInboundOnTheEvent(t *testing.T) {
	n := mustRegister(t, vendorReg())
	in := fullInbound()
	events, err := n.DeclareProfile(withInbound(in), t1)
	if err != nil {
		t.Fatal(err)
	}
	e := events[0].(NodeOperationalProfileDeclared)
	if !reflect.DeepEqual(e.Inbound, fullInbound()) {
		t.Fatalf("event inbound = %+v", e.Inbound)
	}
	// neither the caller's value, the event nor the stored profile alias each other
	*in.MaxCartonWeightG = 1
	*e.Inbound.MaxCartonWeightG = 2
	p, _ := n.Profile()
	if *p.Inbound.MaxCartonWeightG != 25000 {
		t.Fatal("the stored profile aliased the caller's or the event's inbound")
	}
	*p.Inbound.MaxCartonWeightG = 3
	if again, _ := n.Profile(); *again.Inbound.MaxCartonWeightG != 25000 {
		t.Fatal("Profile() must return a deep copy")
	}
}

func TestDeclareProfileWithoutInboundLeavesItNil(t *testing.T) {
	n := mustRegister(t, vendorReg())
	events, err := n.DeclareProfile(validDecl(), t1)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].(NodeOperationalProfileDeclared).Inbound != nil {
		t.Fatal("no inbound declared: the event carries none")
	}
	if p, _ := n.Profile(); p.Inbound != nil {
		t.Fatal("no inbound declared: the profile holds none")
	}
}

func TestDeclareProfileRefusesBadInboundAndChangesNothing(t *testing.T) {
	n := mustRegister(t, vendorReg())
	before := n.Snapshot()
	events, err := n.DeclareProfile(withInbound(&InboundLimits{MaxCartonWeightG: intp(0)}), t1)
	if !errors.Is(err, shared.ErrValidation) || events != nil {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	if !reflect.DeepEqual(n.Snapshot(), before) {
		t.Fatal("a refused declaration changes nothing")
	}
}

func TestSnapshotRestoreKeepsInbound(t *testing.T) {
	n := mustRegister(t, vendorReg())
	if _, err := n.DeclareProfile(withInbound(fullInbound()), t1); err != nil {
		t.Fatal(err)
	}
	s := n.Snapshot()
	if !reflect.DeepEqual(s.Profile.Inbound, fullInbound()) {
		t.Fatalf("snapshot inbound = %+v", s.Profile.Inbound)
	}
}
