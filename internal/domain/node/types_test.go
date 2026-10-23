package node

import (
	"reflect"
	"testing"
)

type typeRow struct {
	typ          Type
	operators    []Operator
	capabilities Capabilities
	subtypes     []Subtype
}

// modelRows transcribes model.yaml node_types; every row is pinned.
func modelRows() []typeRow {
	return []typeRow{
		{FulfillmentCenter, []Operator{OperatorVendor}, Capabilities{Outbound: true},
			[]Subtype{SubtypeSortable, SubtypeNonSortable, SubtypeXL, SubtypeSoftLines, SubtypeSubSameDay, SubtypeFresh, SubtypeMicro}},
		{InboundCrossDock, []Operator{OperatorVendor}, Capabilities{Inbound: true}, nil},
		{ReturnsCenter, []Operator{OperatorVendor}, Capabilities{Returns: true}, nil},
		{SortationCenter, []Operator{OperatorNetwork}, Capabilities{Sort: true}, nil},
		{DeliveryStation, []Operator{OperatorNetwork}, Capabilities{LastMile: true}, nil},
		{AirHub, []Operator{OperatorNetwork}, Capabilities{Sort: true, Air: true}, nil},
	}
}

func TestTypeRulesPinTheModel(t *testing.T) {
	rows := modelRows()
	if len(typeRules) != len(rows) {
		t.Fatalf("typeRules has %d rows, model has %d", len(typeRules), len(rows))
	}
	for _, want := range rows {
		t.Run(string(want.typ), func(t *testing.T) {
			rule, ok := want.typ.Rule()
			if !ok {
				t.Fatal("type missing from the table")
			}
			if !reflect.DeepEqual(rule.Operators, want.operators) {
				t.Errorf("operators = %v, want %v", rule.Operators, want.operators)
			}
			if rule.Capabilities != want.capabilities || want.typ.Capabilities() != want.capabilities {
				t.Errorf("capabilities = %+v, want %+v", rule.Capabilities, want.capabilities)
			}
			if !reflect.DeepEqual(rule.Subtypes, want.subtypes) {
				t.Errorf("subtypes = %v, want %v", rule.Subtypes, want.subtypes)
			}
			if !want.typ.Valid() {
				t.Error("Valid() = false")
			}
		})
	}
}

func TestTypesAreInTheModelOrderAndCopied(t *testing.T) {
	want := []Type{FulfillmentCenter, InboundCrossDock, ReturnsCenter, SortationCenter, DeliveryStation, AirHub}
	got := Types()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Types() = %v", got)
	}
	got[0] = AirHub
	if Types()[0] != FulfillmentCenter {
		t.Fatal("Types() must return a copy")
	}
	rule, _ := FulfillmentCenter.Rule()
	rule.Operators[0], rule.Subtypes[0] = OperatorNetwork, SubtypeMicro
	again, _ := FulfillmentCenter.Rule()
	if again.Operators[0] != OperatorVendor || again.Subtypes[0] != SubtypeSortable {
		t.Fatal("Rule() must return copies")
	}
	if _, ok := Type("WAREHOUSE").Rule(); ok || Type("WAREHOUSE").Valid() || Type("").Valid() {
		t.Fatal("an unknown type has no rule")
	}
	if c := Type("WAREHOUSE").Capabilities(); c != (Capabilities{}) {
		t.Fatalf("an unknown type has no capabilities: %+v", c)
	}
}

func TestOperatorAndSubtypeMatrix(t *testing.T) {
	for _, row := range modelRows() {
		for _, o := range []Operator{OperatorNetwork, OperatorVendor} {
			want := o == row.operators[0]
			if got := row.typ.AllowsOperator(o); got != want {
				t.Errorf("%s.AllowsOperator(%s) = %v, want %v", row.typ, o, got, want)
			}
		}
		for _, s := range []Subtype{SubtypeSortable, SubtypeNonSortable, SubtypeXL, SubtypeSoftLines, SubtypeSubSameDay, SubtypeFresh, SubtypeMicro} {
			want := row.typ == FulfillmentCenter
			if got := row.typ.AllowsSubtype(s); got != want {
				t.Errorf("%s.AllowsSubtype(%s) = %v, want %v", row.typ, s, got, want)
			}
		}
		if row.typ.AllowsOperator("") || row.typ.AllowsSubtype("") || row.typ.AllowsSubtype("HUGE") {
			t.Errorf("%s allows an unknown value", row.typ)
		}
	}
	if Type("WAREHOUSE").AllowsOperator(OperatorVendor) {
		t.Error("an unknown type allows no operator")
	}
}

func TestEnumValidity(t *testing.T) {
	for _, o := range []Operator{OperatorNetwork, OperatorVendor} {
		if !o.Valid() {
			t.Errorf("%s must be valid", o)
		}
	}
	if Operator("").Valid() || Operator("network").Valid() {
		t.Error("unknown operators must be invalid")
	}
	for _, s := range []Status{StatusProposed, StatusActive, StatusSuspended, StatusRetired} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	if Status("").Valid() || Status("DRAFT").Valid() {
		t.Error("unknown statuses must be invalid")
	}
}

func TestTransitionsPinTheModel(t *testing.T) {
	want := []Transition{
		{StatusProposed, StatusActive, "ActivateNode"},
		{StatusActive, StatusSuspended, "SuspendNode"},
		{StatusSuspended, StatusActive, "ActivateNode"},
		{StatusActive, StatusRetired, "RetireNode"},
		{StatusSuspended, StatusRetired, "RetireNode"},
	}
	got := Transitions()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Transitions() = %v", got)
	}
	got[0].From = StatusRetired
	if Transitions()[0].From != StatusProposed {
		t.Fatal("Transitions() must return a copy")
	}
	states := []Status{StatusProposed, StatusActive, StatusSuspended, StatusRetired}
	allowed := 0
	for _, from := range states {
		for _, to := range states {
			isWant := false
			for _, w := range want {
				if w.From == from && w.To == to {
					isWant = true
				}
			}
			if canMove(from, to) != isWant {
				t.Errorf("canMove(%s, %s) = %v, want %v", from, to, !isWant, isWant)
			}
			if isWant {
				allowed++
			}
		}
	}
	if allowed != 5 {
		t.Fatalf("model has 5 transitions, matrix found %d", allowed)
	}
}
