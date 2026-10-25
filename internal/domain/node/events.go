package node

import (
	"time"

	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

const entity = "node"

// NodeRegistered is raised by Register. Its fields are the model's data list;
// Subtype, VendorID and VendorNodeCode are empty (JSON null on the wire) when
// they do not apply. Capabilities are not carried: consumers derive them from
// NodeType.
type NodeRegistered struct {
	shared.Header
	NodeID         ID
	NodeType       Type
	Subtype        Subtype
	Operator       Operator
	VendorID       string
	VendorNodeCode string
	RegionID       region.ID
	Timezone       string
	PostalCode     string
	CountryCode    string
}

// EventName returns "NodeRegistered".
func (NodeRegistered) EventName() string { return "NodeRegistered" }

// Entity returns "node".
func (NodeRegistered) Entity() string { return entity }

// SubjectID returns the nodeId.
func (e NodeRegistered) SubjectID() string { return string(e.NodeID) }

// NodeOperationalProfileDeclared is raised by DeclareProfile with the whole new
// revision. SortDwellMinutes is nil unless the node sorts. The instant of the
// declaration is the header's OccurredAt.
type NodeOperationalProfileDeclared struct {
	shared.Header
	NodeID              ID
	ProfileRevision     int
	Timezone            string
	OperatingHoursByDay OperatingHours
	Holidays            []string
	HandlingTimeMinutes int
	Cutoffs             []CutoffSlot
	ThroughputCap       ThroughputCap
	SortDwellMinutes    *int
	// Inbound is nil unless the revision declares inbound limits.
	Inbound    *InboundLimits
	DeclaredBy DeclaredBy
}

func profileDeclared(h shared.Header, id ID, p OperationalProfile) NodeOperationalProfileDeclared {
	return NodeOperationalProfileDeclared{
		Header: h, NodeID: id, ProfileRevision: p.Revision, Timezone: p.Timezone,
		OperatingHoursByDay: p.OperatingHours, Holidays: p.Holidays, HandlingTimeMinutes: p.HandlingTimeMinutes,
		Cutoffs: p.Cutoffs, ThroughputCap: p.ThroughputCap, SortDwellMinutes: p.SortDwellMinutes, Inbound: p.Inbound, DeclaredBy: p.DeclaredBy,
	}
}

// EventName returns "NodeOperationalProfileDeclared".
func (NodeOperationalProfileDeclared) EventName() string { return "NodeOperationalProfileDeclared" }

// Entity returns "node".
func (NodeOperationalProfileDeclared) Entity() string { return entity }

// SubjectID returns the nodeId.
func (e NodeOperationalProfileDeclared) SubjectID() string { return string(e.NodeID) }

// NodeActivated is raised by Activate.
type NodeActivated struct {
	shared.Header
	NodeID      ID
	ActivatedAt time.Time
}

// EventName returns "NodeActivated".
func (NodeActivated) EventName() string { return "NodeActivated" }

// Entity returns "node".
func (NodeActivated) Entity() string { return entity }

// SubjectID returns the nodeId.
func (e NodeActivated) SubjectID() string { return string(e.NodeID) }

// NodeSuspended is raised by Suspend.
type NodeSuspended struct {
	shared.Header
	NodeID      ID
	Reason      string
	SuspendedAt time.Time
}

// EventName returns "NodeSuspended".
func (NodeSuspended) EventName() string { return "NodeSuspended" }

// Entity returns "node".
func (NodeSuspended) Entity() string { return entity }

// SubjectID returns the nodeId.
func (e NodeSuspended) SubjectID() string { return string(e.NodeID) }

// NodeRetired is raised by Retire.
type NodeRetired struct {
	shared.Header
	NodeID    ID
	RetiredAt time.Time
}

// EventName returns "NodeRetired".
func (NodeRetired) EventName() string { return "NodeRetired" }

// Entity returns "node".
func (NodeRetired) Entity() string { return entity }

// SubjectID returns the nodeId.
func (e NodeRetired) SubjectID() string { return string(e.NodeID) }

var (
	_ shared.Event = NodeRegistered{}
	_ shared.Event = NodeOperationalProfileDeclared{}
	_ shared.Event = NodeActivated{}
	_ shared.Event = NodeSuspended{}
	_ shared.Event = NodeRetired{}
)
