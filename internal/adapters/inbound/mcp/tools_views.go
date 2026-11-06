package mcp

import (
	"time"

	"github.com/claudioed/node-topology/internal/application/views"
)

// --- outputs (the REST bodies, camelCase like the REST contract) --------------

type vendorRefView struct {
	VendorID       string `json:"vendorId"`
	VendorNodeCode string `json:"vendorNodeCode"`
}

type geoLocationView struct {
	PostalCode  string   `json:"postalCode"`
	CountryCode string   `json:"countryCode"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

type capabilitiesView struct {
	Outbound bool `json:"outbound"`
	Inbound  bool `json:"inbound"`
	Returns  bool `json:"returns"`
	Sort     bool `json:"sort"`
	LastMile bool `json:"lastMile"`
	Air      bool `json:"air"`
}

type dayHoursView struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type cutoffSlotView struct {
	LocalTime string   `json:"localTime"`
	Days      []string `json:"days,omitempty"`
	MaxOrders int      `json:"maxOrders,omitempty"`
}

type throughputCapView struct {
	Value    int    `json:"value"`
	TimeUnit string `json:"timeUnit"`
	Unit     string `json:"unit"`
}

type operationalProfileView struct {
	ProfileRevision     int                     `json:"profileRevision"`
	Timezone            string                  `json:"timezone"`
	OperatingHoursByDay map[string]dayHoursView `json:"operatingHoursByDay"`
	Holidays            []string                `json:"holidays"`
	HandlingTimeMinutes int                     `json:"handlingTimeMinutes"`
	Cutoffs             []cutoffSlotView        `json:"cutoffs"`
	ThroughputCap       throughputCapView       `json:"throughputCap"`
	SortDwellMinutes    *int                    `json:"sortDwellMinutes,omitempty"`
	DeclaredBy          string                  `json:"declaredBy"`
	DeclaredAt          string                  `json:"declaredAt"`
}

type nodeView struct {
	NodeID             string                  `json:"nodeId"`
	NodeType           string                  `json:"nodeType"`
	Subtype            string                  `json:"subtype,omitempty"`
	Operator           string                  `json:"operator"`
	VendorRef          *vendorRefView          `json:"vendorRef,omitempty"`
	RegionID           string                  `json:"regionId"`
	Timezone           string                  `json:"timezone"`
	Location           geoLocationView         `json:"location"`
	Capabilities       capabilitiesView        `json:"capabilities"`
	Status             string                  `json:"status"`
	OperationalProfile *operationalProfileView `json:"operationalProfile,omitempty"`
	ActivatedAt        string                  `json:"activatedAt,omitempty"`
	SuspendedAt        string                  `json:"suspendedAt,omitempty"`
	SuspendReason      string                  `json:"suspendReason,omitempty"`
	RetiredAt          string                  `json:"retiredAt,omitempty"`
}

type nodeListView struct {
	Nodes []nodeView `json:"nodes"`
}

type laneView struct {
	LaneID                string `json:"laneId"`
	OriginNodeID          string `json:"originNodeId"`
	DestinationNodeID     string `json:"destinationNodeId"`
	Mode                  string `json:"mode"`
	NominalTransitMinutes int    `json:"nominalTransitMinutes"`
	Status                string `json:"status"`
	CostPerUnitCents      int64  `json:"costPerUnitCents"`
	ClosedAt              string `json:"closedAt,omitempty"`
}

type laneListView struct {
	Lanes []laneView `json:"lanes"`
}

type serviceAreaView struct {
	PostalPrefix          string `json:"postalPrefix"`
	DeliveryStationNodeID string `json:"deliveryStationNodeId"`
	RegionID              string `json:"regionId"`
	AssignmentVersion     int    `json:"assignmentVersion"`
}

type regionView struct {
	RegionID string `json:"regionId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

// regionsListView wraps the region list the way the node and lane lists are
// wrapped: an OBJECT, so a tool's structured content is always an object
// (REST answers {items, nextCursor}; the MCP tool returns every region under
// its own `regions` key).
type regionsListView struct {
	Regions []regionView `json:"regions"`
}

// --- mapping ------------------------------------------------------------------

// instant formats an optional instant as RFC 3339 UTC; nil and the zero time
// are "" (omitted).
func instant(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func toGeoLocation(l views.GeoLocation) geoLocationView {
	return geoLocationView{PostalCode: l.PostalCode, CountryCode: l.CountryCode, Lat: l.Lat, Lon: l.Lon}
}

func toCapabilities(c views.Capabilities) capabilitiesView {
	return capabilitiesView{
		Outbound: c.Outbound, Inbound: c.Inbound, Returns: c.Returns,
		Sort: c.Sort, LastMile: c.LastMile, Air: c.Air,
	}
}

func toProfile(p views.OperationalProfile) operationalProfileView {
	hours := make(map[string]dayHoursView, len(p.OperatingHoursByDay))
	for day, h := range p.OperatingHoursByDay {
		hours[day] = dayHoursView{Start: h.Start, End: h.End}
	}
	cutoffs := make([]cutoffSlotView, len(p.Cutoffs))
	for i, c := range p.Cutoffs {
		slot := cutoffSlotView{LocalTime: c.LocalTime, Days: c.Days, MaxOrders: c.MaxOrders}
		cutoffs[i] = slot
	}
	holidays := append([]string{}, p.Holidays...)
	if holidays == nil {
		holidays = []string{}
	}
	return operationalProfileView{
		ProfileRevision: p.ProfileRevision, Timezone: p.Timezone, OperatingHoursByDay: hours,
		Holidays: holidays, HandlingTimeMinutes: p.HandlingTimeMinutes, Cutoffs: cutoffs,
		ThroughputCap: throughputCapView{
			Value: p.ThroughputCap.Value, TimeUnit: p.ThroughputCap.TimeUnit, Unit: p.ThroughputCap.Unit,
		},
		SortDwellMinutes: p.SortDwellMinutes, DeclaredBy: p.DeclaredBy, DeclaredAt: instant(&p.DeclaredAt),
	}
}

// toNode maps the REST view; the nested collections are never nil.
func toNode(n views.Node) nodeView {
	out := nodeView{
		NodeID: n.NodeID, NodeType: n.NodeType, Subtype: n.Subtype, Operator: n.Operator,
		RegionID: n.RegionID, Timezone: n.Timezone, Location: toGeoLocation(n.Location),
		Capabilities: toCapabilities(n.Capabilities), Status: n.Status, SuspendReason: n.SuspendReason,
		ActivatedAt: instant(n.ActivatedAt), SuspendedAt: instant(n.SuspendedAt), RetiredAt: instant(n.RetiredAt),
	}
	if n.VendorRef != nil {
		out.VendorRef = &vendorRefView{VendorID: n.VendorRef.VendorID, VendorNodeCode: n.VendorRef.VendorNodeCode}
	}
	if n.OperationalProfile != nil {
		profile := toProfile(*n.OperationalProfile)
		out.OperationalProfile = &profile
	}
	return out
}

func toLane(l views.Lane) laneView {
	return laneView{
		LaneID: l.LaneID, OriginNodeID: l.OriginNodeID, DestinationNodeID: l.DestinationNodeID,
		Mode: l.Mode, NominalTransitMinutes: l.NominalTransitMinutes, Status: l.Status,
		CostPerUnitCents: l.CostPerUnitCents, ClosedAt: instant(l.ClosedAt),
	}
}

func toServiceArea(a views.ServiceArea) serviceAreaView {
	return serviceAreaView{
		PostalPrefix: a.PostalPrefix, DeliveryStationNodeID: a.DeliveryStationNodeID,
		RegionID: a.RegionID, AssignmentVersion: a.AssignmentVersion,
	}
}

func toRegion(r views.Region) regionView {
	return regionView{RegionID: r.RegionID, Code: r.Code, Name: r.Name}
}
