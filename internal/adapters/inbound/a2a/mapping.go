package a2a

import (
	"fmt"
	"strings"
	"time"

	"github.com/claudioed/node-topology/internal/application/views"
)

// problem is the RFC 9457-shaped rejection an agent client receives instead of
// a result: domain rejections and request-shape failures are business answers,
// not transport errors, so they travel in the message envelope. Type is the
// stable slug, like the REST adapter's problem `type`.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// summary renders the one-line form used for the reply's text part.
func (p *problem) summary() string {
	return fmt.Sprintf("%s (%d): %s", p.Title, p.Status, p.Detail)
}

func problemMalformedRequest(detail string) *problem {
	return &problem{Type: "malformed-skill-request", Title: "Malformed skill request", Status: 400, Detail: detail}
}

func problemUnknownSkill(skill string) *problem {
	return &problem{
		Type: "unknown-skill", Title: "Unknown skill", Status: 400,
		Detail: fmt.Sprintf("skill %q is not offered by this agent; the Agent Card lists the supported skills", skill),
	}
}

func problemInvalidArgument(detail string) *problem {
	return &problem{Type: "invalid-argument", Title: "Invalid skill argument", Status: 400, Detail: detail}
}

// envelope is the structured payload of every skill answer: exactly one of
// Data (a skill DTO) or Problem (why there is no result), plus the skill id
// the answer belongs to.
type envelope struct {
	Skill   string   `json:"skill"`
	Data    skillDTO `json:"data,omitzero"`
	Problem *problem `json:"problem,omitempty"`
}

// skillDTO is the union of the skill result DTOs: each skill fills its own
// field, keeping the envelope flat and versioned as one contract.
type skillDTO struct {
	ServingStation *servingStation `json:"servingStation,omitempty"`
	Nodes          *nodeList       `json:"nodeList,omitempty"`
	summary        string
}

// servingStation is the composed read DTO of the serving_station skill.
type servingStation struct {
	PostalPrefix          string   `json:"postalPrefix"`
	DeliveryStationNodeID string   `json:"deliveryStationNodeId"`
	RegionID              string   `json:"regionId"`
	AssignmentVersion     int      `json:"assignmentVersion"`
	Station               nodeView `json:"station"`
}

// nodeList is the DTO of the find_nodes skill.
type nodeList struct {
	Count int        `json:"count"`
	Nodes []nodeView `json:"nodes"`
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

type profileView struct {
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

// nodeView is the node DTO of both skills. OperationalProfile is carried only
// where the skill asks for it (serving_station); find_nodes carries the
// profile revision alone, so a listing stays small.
type nodeView struct {
	NodeID          string           `json:"nodeId"`
	NodeType        string           `json:"nodeType"`
	Subtype         string           `json:"subtype,omitempty"`
	Operator        string           `json:"operator"`
	RegionID        string           `json:"regionId"`
	Timezone        string           `json:"timezone"`
	PostalCode      string           `json:"postalCode"`
	CountryCode     string           `json:"countryCode"`
	Capabilities    capabilitiesView `json:"capabilities"`
	Status          string           `json:"status"`
	ProfileRevision int              `json:"profileRevision,omitempty"`
	Profile         *profileView     `json:"operationalProfile,omitempty"`
}

// instant formats an instant as RFC 3339 UTC; the zero time is "".
func instant(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func toProfile(p views.OperationalProfile) profileView {
	hours := make(map[string]dayHoursView, len(p.OperatingHoursByDay))
	for day, h := range p.OperatingHoursByDay {
		hours[day] = dayHoursView{Start: h.Start, End: h.End}
	}
	cutoffs := make([]cutoffSlotView, len(p.Cutoffs))
	for i, c := range p.Cutoffs {
		cutoffs[i] = cutoffSlotView{LocalTime: c.LocalTime, Days: c.Days, MaxOrders: c.MaxOrders}
	}
	holidays := append([]string{}, p.Holidays...)
	return profileView{
		ProfileRevision: p.ProfileRevision, Timezone: p.Timezone, OperatingHoursByDay: hours,
		Holidays: holidays, HandlingTimeMinutes: p.HandlingTimeMinutes, Cutoffs: cutoffs,
		ThroughputCap: throughputCapView{
			Value: p.ThroughputCap.Value, TimeUnit: p.ThroughputCap.TimeUnit, Unit: p.ThroughputCap.Unit,
		},
		SortDwellMinutes: p.SortDwellMinutes, DeclaredBy: p.DeclaredBy, DeclaredAt: instant(p.DeclaredAt),
	}
}

// toNode maps the read view; the profile is carried in full only when
// withProfile is set. Domain and view structs never cross the boundary.
func toNode(n views.Node, withProfile bool) nodeView {
	out := nodeView{
		NodeID: n.NodeID, NodeType: n.NodeType, Subtype: n.Subtype, Operator: n.Operator,
		RegionID: n.RegionID, Timezone: n.Timezone,
		PostalCode: n.Location.PostalCode, CountryCode: n.Location.CountryCode,
		Capabilities: capabilitiesView{
			Outbound: n.Capabilities.Outbound, Inbound: n.Capabilities.Inbound, Returns: n.Capabilities.Returns,
			Sort: n.Capabilities.Sort, LastMile: n.Capabilities.LastMile, Air: n.Capabilities.Air,
		},
		Status: n.Status,
	}
	if n.OperationalProfile != nil {
		out.ProfileRevision = n.OperationalProfile.ProfileRevision
		if withProfile {
			profile := toProfile(*n.OperationalProfile)
			out.Profile = &profile
		}
	}
	return out
}

// toServingStation composes the service area and its station node.
func toServingStation(area views.ServiceArea, station views.Node) servingStation {
	return servingStation{
		PostalPrefix: area.PostalPrefix, DeliveryStationNodeID: area.DeliveryStationNodeID,
		RegionID: area.RegionID, AssignmentVersion: area.AssignmentVersion,
		Station: toNode(station, true),
	}
}

// summary is the one-line text of a serving_station answer.
func (s servingStation) summary() string {
	return fmt.Sprintf("postal prefix %s is served by %s (%s, %s) in region %s",
		s.PostalPrefix, s.DeliveryStationNodeID, s.Station.NodeType, s.Station.Status, s.RegionID)
}

// summary is the one-line text of a find_nodes answer.
func (l nodeList) summary() string {
	if l.Count == 0 {
		return "no node matches"
	}
	ids := make([]string, len(l.Nodes))
	for i, n := range l.Nodes {
		ids[i] = n.NodeID
	}
	return fmt.Sprintf("%d node(s): %s", l.Count, strings.Join(ids, ", "))
}
