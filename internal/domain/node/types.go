package node

import "slices"

// Type is a node type of the model's node_types table.
type Type string

// The six node types.
const (
	FulfillmentCenter Type = "FULFILLMENT_CENTER"
	InboundCrossDock  Type = "INBOUND_CROSS_DOCK"
	ReturnsCenter     Type = "RETURNS_CENTER"
	SortationCenter   Type = "SORTATION_CENTER"
	DeliveryStation   Type = "DELIVERY_STATION"
	AirHub            Type = "AIR_HUB"
)

// Operator says who runs the building.
type Operator string

// The two operators.
const (
	OperatorNetwork Operator = "NETWORK"
	OperatorVendor  Operator = "VENDOR"
)

// Subtype refines a FULFILLMENT_CENTER.
type Subtype string

// The fulfillment-centre subtypes.
const (
	SubtypeSortable    Subtype = "SORTABLE"
	SubtypeNonSortable Subtype = "NON_SORTABLE"
	SubtypeXL          Subtype = "XL"
	SubtypeSoftLines   Subtype = "SOFT_LINES"
	SubtypeSubSameDay  Subtype = "SUB_SAME_DAY"
	SubtypeFresh       Subtype = "FRESH"
	SubtypeMicro       Subtype = "MICRO"
)

// Status is the lifecycle state of a node.
type Status string

// The four node states.
const (
	StatusProposed  Status = "PROPOSED"
	StatusActive    Status = "ACTIVE"
	StatusSuspended Status = "SUSPENDED"
	StatusRetired   Status = "RETIRED"
)

// Capabilities are what a node can do, derived from its type
// (policy DefaultCapabilitiesByType) and never set by a caller.
type Capabilities struct {
	Outbound bool
	Inbound  bool
	Returns  bool
	Sort     bool
	LastMile bool
	Air      bool
}

// TypeRule is one row of the model's node_types table.
type TypeRule struct {
	Operators    []Operator
	Capabilities Capabilities
	Subtypes     []Subtype
}

// allTypes lists the types in the model's order.
var allTypes = []Type{
	FulfillmentCenter, InboundCrossDock, ReturnsCenter, SortationCenter, DeliveryStation, AirHub,
}

// typeRules embeds the model's node_types: operator_allowed, capabilities and
// subtypes of every node type. TestTypeRulesPinTheModel pins each row.
var typeRules = map[Type]TypeRule{
	FulfillmentCenter: {
		Operators:    []Operator{OperatorVendor},
		Capabilities: Capabilities{Outbound: true},
		Subtypes: []Subtype{
			SubtypeSortable, SubtypeNonSortable, SubtypeXL, SubtypeSoftLines,
			SubtypeSubSameDay, SubtypeFresh, SubtypeMicro,
		},
	},
	InboundCrossDock: {Operators: []Operator{OperatorVendor}, Capabilities: Capabilities{Inbound: true}},
	ReturnsCenter:    {Operators: []Operator{OperatorVendor}, Capabilities: Capabilities{Returns: true}},
	SortationCenter:  {Operators: []Operator{OperatorNetwork}, Capabilities: Capabilities{Sort: true}},
	DeliveryStation:  {Operators: []Operator{OperatorNetwork}, Capabilities: Capabilities{LastMile: true}},
	AirHub:           {Operators: []Operator{OperatorNetwork}, Capabilities: Capabilities{Sort: true, Air: true}},
}

// Types returns every node type in the model's order.
func Types() []Type { return slices.Clone(allTypes) }

// Valid reports whether t is one of the six node types.
func (t Type) Valid() bool {
	_, ok := typeRules[t]
	return ok
}

// Rule returns a copy of the type's row of the node_types table.
func (t Type) Rule() (TypeRule, bool) {
	r, ok := typeRules[t]
	if !ok {
		return TypeRule{}, false
	}
	r.Operators = slices.Clone(r.Operators)
	r.Subtypes = slices.Clone(r.Subtypes)
	return r, true
}

// Capabilities returns the capabilities derived from the type.
func (t Type) Capabilities() Capabilities { return typeRules[t].Capabilities }

// AllowsOperator reports whether the type may be run by o.
func (t Type) AllowsOperator(o Operator) bool { return slices.Contains(typeRules[t].Operators, o) }

// AllowsSubtype reports whether s is one of the type's subtypes.
func (t Type) AllowsSubtype(s Subtype) bool { return slices.Contains(typeRules[t].Subtypes, s) }

// Valid reports whether o is NETWORK or VENDOR.
func (o Operator) Valid() bool { return o == OperatorNetwork || o == OperatorVendor }

// Valid reports whether s is one of the four states.
func (s Status) Valid() bool {
	return s == StatusProposed || s == StatusActive || s == StatusSuspended || s == StatusRetired
}

// Transition is one row of the model's Node transition table.
type Transition struct {
	From    Status
	To      Status
	Command string
}

var transitions = []Transition{
	{StatusProposed, StatusActive, "ActivateNode"},
	{StatusActive, StatusSuspended, "SuspendNode"},
	{StatusSuspended, StatusActive, "ActivateNode"},
	{StatusActive, StatusRetired, "RetireNode"},
	{StatusSuspended, StatusRetired, "RetireNode"},
}

// Transitions returns the model's transition table.
func Transitions() []Transition { return slices.Clone(transitions) }

func canMove(from, to Status) bool {
	return slices.ContainsFunc(transitions, func(t Transition) bool { return t.From == from && t.To == to })
}
