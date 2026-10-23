// Package views holds the read DTOs the use cases return: one per schema of
// apis/openapi.yaml (Node, Lane, ServiceArea, Region). The JSON tags are the
// REST wire names, so the HTTP adapter marshals them as they are. A view is a
// copy; nothing in it aliases an aggregate.
package views

import (
	"time"

	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// VendorRef is the vendor's own code of a node.
type VendorRef struct {
	VendorID       string `json:"vendorId"`
	VendorNodeCode string `json:"vendorNodeCode"`
}

// GeoLocation is where a node is.
type GeoLocation struct {
	PostalCode  string   `json:"postalCode"`
	CountryCode string   `json:"countryCode"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

// Capabilities are what a node can do.
type Capabilities struct {
	Outbound bool `json:"outbound"`
	Inbound  bool `json:"inbound"`
	Returns  bool `json:"returns"`
	Sort     bool `json:"sort"`
	LastMile bool `json:"lastMile"`
	Air      bool `json:"air"`
}

// DayHours is the opening interval of one weekday.
type DayHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// CutoffSlot is one cutoff of a profile.
type CutoffSlot struct {
	LocalTime string   `json:"localTime"`
	Days      []string `json:"days,omitempty"`
	MaxOrders int      `json:"maxOrders,omitempty"`
}

// ThroughputCap is the declared maximum orders per time unit.
type ThroughputCap struct {
	Value    int    `json:"value"`
	TimeUnit string `json:"timeUnit"`
	Unit     string `json:"unit"`
}

// OperationalProfile is one revision of a node's declared profile.
type OperationalProfile struct {
	ProfileRevision     int                 `json:"profileRevision"`
	Timezone            string              `json:"timezone"`
	OperatingHoursByDay map[string]DayHours `json:"operatingHoursByDay"`
	Holidays            []string            `json:"holidays"`
	HandlingTimeMinutes int                 `json:"handlingTimeMinutes"`
	Cutoffs             []CutoffSlot        `json:"cutoffs"`
	ThroughputCap       ThroughputCap       `json:"throughputCap"`
	SortDwellMinutes    *int                `json:"sortDwellMinutes,omitempty"`
	DeclaredBy          string              `json:"declaredBy"`
	DeclaredAt          time.Time           `json:"declaredAt"`
}

// Node is a node with its current operational profile.
type Node struct {
	NodeID             string              `json:"nodeId"`
	NodeType           string              `json:"nodeType"`
	Subtype            string              `json:"subtype,omitempty"`
	Operator           string              `json:"operator"`
	VendorRef          *VendorRef          `json:"vendorRef,omitempty"`
	RegionID           string              `json:"regionId"`
	Timezone           string              `json:"timezone"`
	Location           GeoLocation         `json:"location"`
	Capabilities       Capabilities        `json:"capabilities"`
	Status             string              `json:"status"`
	OperationalProfile *OperationalProfile `json:"operationalProfile,omitempty"`
	ActivatedAt        *time.Time          `json:"activatedAt,omitempty"`
	SuspendedAt        *time.Time          `json:"suspendedAt,omitempty"`
	SuspendReason      string              `json:"suspendReason,omitempty"`
	RetiredAt          *time.Time          `json:"retiredAt,omitempty"`
}

// Lane is a directed connection between two nodes.
type Lane struct {
	LaneID                string     `json:"laneId"`
	OriginNodeID          string     `json:"originNodeId"`
	DestinationNodeID     string     `json:"destinationNodeId"`
	Mode                  string     `json:"mode"`
	NominalTransitMinutes int        `json:"nominalTransitMinutes"`
	Status                string     `json:"status"`
	ClosedAt              *time.Time `json:"closedAt,omitempty"`
}

// ServiceArea is the current assignment of a postal prefix.
type ServiceArea struct {
	PostalPrefix          string `json:"postalPrefix"`
	DeliveryStationNodeID string `json:"deliveryStationNodeId"`
	RegionID              string `json:"regionId"`
	AssignmentVersion     int    `json:"assignmentVersion"`
}

// Region is a defined region.
type Region struct {
	RegionID string `json:"regionId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

// NodeFrom builds the view of n.
func NodeFrom(n *node.Node) Node {
	caps := n.Capabilities()
	loc := n.Location()
	v := Node{
		NodeID:   string(n.ID()),
		NodeType: string(n.Type()),
		Subtype:  string(n.Subtype()),
		Operator: string(n.Operator()),
		RegionID: string(n.RegionID()),
		Timezone: n.Timezone(),
		Location: GeoLocation{PostalCode: loc.PostalCode, CountryCode: loc.CountryCode, Lat: loc.Lat, Lon: loc.Lon},
		Capabilities: Capabilities{
			Outbound: caps.Outbound, Inbound: caps.Inbound, Returns: caps.Returns,
			Sort: caps.Sort, LastMile: caps.LastMile, Air: caps.Air,
		},
		Status:        string(n.Status()),
		SuspendReason: n.SuspendReason(),
		ActivatedAt:   optTime(n.ActivatedAt()),
		SuspendedAt:   optTime(n.SuspendedAt()),
		RetiredAt:     optTime(n.RetiredAt()),
	}
	if ref, ok := n.Vendor(); ok {
		v.VendorRef = &VendorRef{VendorID: ref.VendorID, VendorNodeCode: ref.VendorNodeCode}
	}
	if p, ok := n.Profile(); ok {
		pv := ProfileFrom(p)
		v.OperationalProfile = &pv
	}
	return v
}

// ProfileFrom builds the view of one profile revision.
func ProfileFrom(p node.OperationalProfile) OperationalProfile {
	hours := make(map[string]DayHours, len(p.OperatingHours))
	for day, h := range p.OperatingHours {
		hours[string(day)] = DayHours{Start: string(h.Start), End: string(h.End)}
	}
	cutoffs := make([]CutoffSlot, len(p.Cutoffs))
	for i, c := range p.Cutoffs {
		cs := CutoffSlot{LocalTime: string(c.LocalTime), MaxOrders: c.MaxOrders}
		for _, d := range c.Days {
			cs.Days = append(cs.Days, string(d))
		}
		cutoffs[i] = cs
	}
	return OperationalProfile{
		ProfileRevision:     p.Revision,
		Timezone:            p.Timezone,
		OperatingHoursByDay: hours,
		Holidays:            append([]string{}, p.Holidays...),
		HandlingTimeMinutes: p.HandlingTimeMinutes,
		Cutoffs:             cutoffs,
		ThroughputCap: ThroughputCap{
			Value: p.ThroughputCap.Value, TimeUnit: string(p.ThroughputCap.TimeUnit), Unit: p.ThroughputCap.Unit,
		},
		SortDwellMinutes: copyInt(p.SortDwellMinutes),
		DeclaredBy:       string(p.DeclaredBy),
		DeclaredAt:       p.DeclaredAt,
	}
}

// LaneFrom builds the view of l.
func LaneFrom(l *lane.Lane) Lane {
	return Lane{
		LaneID:                string(l.ID()),
		OriginNodeID:          string(l.OriginNodeID()),
		DestinationNodeID:     string(l.DestinationNodeID()),
		Mode:                  string(l.Mode()),
		NominalTransitMinutes: l.NominalTransitMinutes(),
		Status:                string(l.Status()),
		ClosedAt:              optTime(l.ClosedAt()),
	}
}

// ServiceAreaFrom builds the view of s.
func ServiceAreaFrom(s *servicearea.ServiceArea) ServiceArea {
	return ServiceArea{
		PostalPrefix:          string(s.PostalPrefix()),
		DeliveryStationNodeID: string(s.DeliveryStationNodeID()),
		RegionID:              string(s.RegionID()),
		AssignmentVersion:     s.AssignmentVersion(),
	}
}

// RegionFrom builds the view of r.
func RegionFrom(r *region.Region) Region {
	return Region{RegionID: string(r.ID()), Code: r.Code(), Name: r.Name()}
}

// RegionsFrom builds the views of rs, in the given order. The result is never
// nil, so an empty list marshals as [].
func RegionsFrom(rs []*region.Region) []Region {
	out := make([]Region, len(rs))
	for i, r := range rs {
		out[i] = RegionFrom(r)
	}
	return out
}

// optTime returns nil for the zero time.
func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
