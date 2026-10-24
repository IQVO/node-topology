package kafka

import (
	"github.com/claudioed/node-topology/internal/domain/node"
)

// Wire payloads (CloudEvents `data`, camelCase JSON), exactly the ones pinned
// in apis/asyncapi.yaml. Optional fields are omitted when unset, except the
// three nullable NodeRegistered fields (subtype, vendorId, vendorNodeCode),
// which are always present and null when they do not apply (ADR 0004).
// Arrays are always present. Instants are RFC 3339 UTC strings.

type registeredPayload struct {
	NodeID         string  `json:"nodeId"`
	NodeType       string  `json:"nodeType"`
	Subtype        *string `json:"subtype"`
	Operator       string  `json:"operator"`
	VendorID       *string `json:"vendorId"`
	VendorNodeCode *string `json:"vendorNodeCode"`
	RegionID       string  `json:"regionId"`
	Timezone       string  `json:"timezone"`
	PostalCode     string  `json:"postalCode"`
	CountryCode    string  `json:"countryCode"`
}

type dayHoursPayload struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// operatingHoursPayload lists the weekdays in calendar order (Monday first),
// so the JSON is byte-stable; a closed weekday is omitted.
type operatingHoursPayload struct {
	Monday    *dayHoursPayload `json:"monday,omitempty"`
	Tuesday   *dayHoursPayload `json:"tuesday,omitempty"`
	Wednesday *dayHoursPayload `json:"wednesday,omitempty"`
	Thursday  *dayHoursPayload `json:"thursday,omitempty"`
	Friday    *dayHoursPayload `json:"friday,omitempty"`
	Saturday  *dayHoursPayload `json:"saturday,omitempty"`
	Sunday    *dayHoursPayload `json:"sunday,omitempty"`
}

type cutoffPayload struct {
	LocalTime string   `json:"localTime"`
	Days      []string `json:"days,omitempty"`
	MaxOrders int      `json:"maxOrders,omitempty"`
}

type throughputPayload struct {
	Value    int    `json:"value"`
	TimeUnit string `json:"timeUnit"`
	Unit     string `json:"unit"`
}

type cartonDimsPayload struct {
	L int `json:"l"`
	W int `json:"w"`
	H int `json:"h"`
}

type inboundPayload struct {
	MaxCartonWeightG *int               `json:"maxCartonWeightG,omitempty"`
	MaxCartonDimsMm  *cartonDimsPayload `json:"maxCartonDimsMm,omitempty"`
	AcceptsPallets   *bool              `json:"acceptsPallets,omitempty"`
	RequiresSscc     *bool              `json:"requiresSscc,omitempty"`
}

func inboundData(l *node.InboundLimits) *inboundPayload {
	if l == nil {
		return nil
	}
	p := &inboundPayload{MaxCartonWeightG: l.MaxCartonWeightG, AcceptsPallets: l.AcceptsPallets, RequiresSscc: l.RequiresSscc}
	if d := l.MaxCartonDimsMm; d != nil {
		p.MaxCartonDimsMm = &cartonDimsPayload{L: d.L, W: d.W, H: d.H}
	}
	return p
}

type profileDeclaredPayload struct {
	NodeID              string                `json:"nodeId"`
	ProfileRevision     int                   `json:"profileRevision"`
	Timezone            string                `json:"timezone"`
	OperatingHoursByDay operatingHoursPayload `json:"operatingHoursByDay"`
	Holidays            []string              `json:"holidays"`
	HandlingTimeMinutes int                   `json:"handlingTimeMinutes"`
	Cutoffs             []cutoffPayload       `json:"cutoffs"`
	ThroughputCap       throughputPayload     `json:"throughputCap"`
	SortDwellMinutes    *int                  `json:"sortDwellMinutes,omitempty"`
	Inbound             *inboundPayload       `json:"inbound,omitempty"`
	DeclaredBy          string                `json:"declaredBy"`
}

type activatedPayload struct {
	NodeID      string `json:"nodeId"`
	ActivatedAt string `json:"activatedAt"`
}

type suspendedPayload struct {
	NodeID      string `json:"nodeId"`
	Reason      string `json:"reason"`
	SuspendedAt string `json:"suspendedAt"`
}

type retiredPayload struct {
	NodeID    string `json:"nodeId"`
	RetiredAt string `json:"retiredAt"`
}

type laneOpenedPayload struct {
	LaneID                string `json:"laneId"`
	OriginNodeID          string `json:"originNodeId"`
	DestinationNodeID     string `json:"destinationNodeId"`
	Mode                  string `json:"mode"`
	NominalTransitMinutes int    `json:"nominalTransitMinutes"`
}

type laneClosedPayload struct {
	LaneID   string `json:"laneId"`
	ClosedAt string `json:"closedAt"`
}

type serviceAreaPayload struct {
	PostalPrefix          string `json:"postalPrefix"`
	DeliveryStationNodeID string `json:"deliveryStationNodeId"`
	RegionID              string `json:"regionId"`
	AssignmentVersion     int    `json:"assignmentVersion"`
}

type regionPayload struct {
	RegionID string `json:"regionId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}

// nullable returns nil for an empty string, so it marshals as JSON null.
func nullable[S ~string](s S) *string {
	if s == "" {
		return nil
	}
	v := string(s)
	return &v
}

func registeredData(e node.NodeRegistered) registeredPayload {
	return registeredPayload{
		NodeID: string(e.NodeID), NodeType: string(e.NodeType), Subtype: nullable(e.Subtype), Operator: string(e.Operator),
		VendorID: nullable(e.VendorID), VendorNodeCode: nullable(e.VendorNodeCode), RegionID: string(e.RegionID),
		Timezone: e.Timezone, PostalCode: e.PostalCode, CountryCode: e.CountryCode,
	}
}

func profileDeclaredData(e node.NodeOperationalProfileDeclared) profileDeclaredPayload {
	return profileDeclaredPayload{
		NodeID: string(e.NodeID), ProfileRevision: e.ProfileRevision, Timezone: e.Timezone,
		OperatingHoursByDay: operatingHours(e.OperatingHoursByDay), Holidays: nonNil(e.Holidays),
		HandlingTimeMinutes: e.HandlingTimeMinutes, Cutoffs: cutoffs(e.Cutoffs),
		ThroughputCap:    throughputPayload{Value: e.ThroughputCap.Value, TimeUnit: string(e.ThroughputCap.TimeUnit), Unit: e.ThroughputCap.Unit},
		SortDwellMinutes: e.SortDwellMinutes, Inbound: inboundData(e.Inbound), DeclaredBy: string(e.DeclaredBy),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func dayHours(h node.OperatingHours, d node.Weekday) *dayHoursPayload {
	v, ok := h[d]
	if !ok {
		return nil
	}
	return &dayHoursPayload{Start: string(v.Start), End: string(v.End)}
}

func operatingHours(h node.OperatingHours) operatingHoursPayload {
	return operatingHoursPayload{
		Monday: dayHours(h, node.Monday), Tuesday: dayHours(h, node.Tuesday), Wednesday: dayHours(h, node.Wednesday),
		Thursday: dayHours(h, node.Thursday), Friday: dayHours(h, node.Friday), Saturday: dayHours(h, node.Saturday),
		Sunday: dayHours(h, node.Sunday),
	}
}

func cutoffs(in []node.CutoffSlot) []cutoffPayload {
	out := make([]cutoffPayload, len(in))
	for i, c := range in {
		out[i] = cutoffPayload{LocalTime: string(c.LocalTime), MaxOrders: c.MaxOrders}
		for _, d := range c.Days {
			out[i].Days = append(out[i].Days, string(d))
		}
	}
	return out
}
