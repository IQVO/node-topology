package http

import (
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// Request bodies, exactly the schemas of apis/openapi.yaml. The decoder is
// strict: an unknown field is 400 malformed-request-body, a missing required
// field reaches the domain and is 422 validation-failed.

type vendorRefBody struct {
	VendorID       string `json:"vendorId"`
	VendorNodeCode string `json:"vendorNodeCode"`
}

type locationBody struct {
	PostalCode  string   `json:"postalCode"`
	CountryCode string   `json:"countryCode"`
	Lat         *float64 `json:"lat"`
	Lon         *float64 `json:"lon"`
}

type registerNodeBody struct {
	NodeType  string         `json:"nodeType"`
	Subtype   string         `json:"subtype"`
	Operator  string         `json:"operator"`
	VendorRef *vendorRefBody `json:"vendorRef"`
	RegionID  string         `json:"regionId"`
	Timezone  string         `json:"timezone"`
	Location  locationBody   `json:"location"`
}

func (b registerNodeBody) command() usecases.RegisterNodeCommand {
	cmd := usecases.RegisterNodeCommand{
		Type: node.Type(b.NodeType), Subtype: node.Subtype(b.Subtype), Operator: node.Operator(b.Operator),
		RegionID: region.ID(b.RegionID), Timezone: b.Timezone,
		Location: node.GeoLocation{PostalCode: b.Location.PostalCode, CountryCode: b.Location.CountryCode, Lat: b.Location.Lat, Lon: b.Location.Lon},
	}
	if b.VendorRef != nil {
		cmd.Vendor = &node.VendorRef{VendorID: b.VendorRef.VendorID, VendorNodeCode: b.VendorRef.VendorNodeCode}
	}
	return cmd
}

type dayHoursBody struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type cutoffBody struct {
	LocalTime string   `json:"localTime"`
	Days      []string `json:"days"`
	MaxOrders int      `json:"maxOrders"`
}

type throughputBody struct {
	Value    int    `json:"value"`
	TimeUnit string `json:"timeUnit"`
	Unit     string `json:"unit"`
}

type profileBody struct {
	ProfileRevision     int                     `json:"profileRevision"`
	Timezone            string                  `json:"timezone"`
	OperatingHoursByDay map[string]dayHoursBody `json:"operatingHoursByDay"`
	Holidays            []string                `json:"holidays"`
	HandlingTimeMinutes int                     `json:"handlingTimeMinutes"`
	Cutoffs             []cutoffBody            `json:"cutoffs"`
	ThroughputCap       throughputBody          `json:"throughputCap"`
	SortDwellMinutes    *int                    `json:"sortDwellMinutes"`
	DeclaredBy          string                  `json:"declaredBy"`
}

func (b profileBody) declaration() node.ProfileDeclaration {
	hours := make(node.OperatingHours, len(b.OperatingHoursByDay))
	for day, h := range b.OperatingHoursByDay {
		hours[node.Weekday(day)] = node.DayHours{Start: node.TimeOfDay(h.Start), End: node.TimeOfDay(h.End)}
	}
	cutoffs := make([]node.CutoffSlot, len(b.Cutoffs))
	for i, c := range b.Cutoffs {
		cutoffs[i] = node.CutoffSlot{LocalTime: node.TimeOfDay(c.LocalTime), MaxOrders: c.MaxOrders}
		for _, d := range c.Days {
			cutoffs[i].Days = append(cutoffs[i].Days, node.Weekday(d))
		}
	}
	return node.ProfileDeclaration{
		Revision: b.ProfileRevision, Timezone: b.Timezone, OperatingHours: hours, Holidays: b.Holidays,
		HandlingTimeMinutes: b.HandlingTimeMinutes, Cutoffs: cutoffs,
		ThroughputCap:    node.ThroughputCap{Value: b.ThroughputCap.Value, TimeUnit: node.TimeUnit(b.ThroughputCap.TimeUnit), Unit: b.ThroughputCap.Unit},
		SortDwellMinutes: b.SortDwellMinutes, DeclaredBy: node.DeclaredBy(b.DeclaredBy),
	}
}

type suspendBody struct {
	Reason string `json:"reason"`
}

type openLaneBody struct {
	OriginNodeID          string `json:"originNodeId"`
	DestinationNodeID     string `json:"destinationNodeId"`
	Mode                  string `json:"mode"`
	NominalTransitMinutes int    `json:"nominalTransitMinutes"`
}

func (b openLaneBody) command() usecases.OpenLaneCommand {
	return usecases.OpenLaneCommand{
		OriginNodeID: node.ID(b.OriginNodeID), DestinationNodeID: node.ID(b.DestinationNodeID),
		Mode: lane.Mode(b.Mode), NominalTransitMinutes: b.NominalTransitMinutes,
	}
}

type assignServiceAreaBody struct {
	DeliveryStationNodeID string `json:"deliveryStationNodeId"`
	AssignmentVersion     int    `json:"assignmentVersion"`
}

func (b assignServiceAreaBody) command(prefix string) usecases.AssignServiceAreaCommand {
	return usecases.AssignServiceAreaCommand{
		PostalPrefix: servicearea.PostalPrefix(prefix), DeliveryStationNodeID: node.ID(b.DeliveryStationNodeID),
		AssignmentVersion: b.AssignmentVersion,
	}
}

type defineRegionBody struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (b defineRegionBody) command() usecases.DefineRegionCommand {
	return usecases.DefineRegionCommand{Code: b.Code, Name: b.Name}
}

// nodeList and laneList wrap the list responses (apis/openapi.yaml); GET
// /regions answers a bare array.
type nodeList struct {
	Nodes []views.Node `json:"nodes"`
}

type laneList struct {
	Lanes []views.Lane `json:"lanes"`
}
