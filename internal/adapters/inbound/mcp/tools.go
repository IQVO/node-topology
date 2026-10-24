package mcp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// Deps is everything the MCP tools need, injected by the composition root.
// It carries the SAME read use cases the REST adapter exposes (GET /nodes,
// GET /nodes/{nodeId}, GET /lanes, GET /service-areas/{postalPrefix},
// GET /regions, GET /regions/{regionId}); the adapter never constructs an
// outbound adapter itself, and it holds no write use case at all
// (docs/adr/0008).
type Deps struct {
	GetNode            *usecases.GetNode
	ListNodes          *usecases.ListNodes
	ListLanes          *usecases.ListLanes
	ResolveServiceArea *usecases.ResolveServiceArea
	GetRegion          *usecases.GetRegion
	ListRegions        *usecases.ListRegions
}

// --- inputs -------------------------------------------------------------------

type nodeIDInput struct {
	NodeID string `json:"nodeId" jsonschema:"the node id, e.g. node-3f6a"`
}

// listNodesInput mirrors the GET /nodes query string: every filter is
// optional and the set ones combine with AND.
type listNodesInput struct {
	Type     string `json:"type,omitempty" jsonschema:"optional filter: keep only nodes of this type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB); omitted means every type"`
	RegionID string `json:"regionId,omitempty" jsonschema:"optional filter: keep only nodes of this region; omitted means every region"`
	Status   string `json:"status,omitempty" jsonschema:"optional filter: keep only nodes in this status (PROPOSED, ACTIVE, SUSPENDED or RETIRED); omitted means every status"`
}

type listLanesInput struct {
	Origin      string `json:"origin,omitempty" jsonschema:"optional filter: keep only lanes leaving this origin node; omitted means every origin"`
	Destination string `json:"destination,omitempty" jsonschema:"optional filter: keep only lanes arriving at this destination node; omitted means every destination"`
}

type postalPrefixInput struct {
	PostalPrefix string `json:"postalPrefix" jsonschema:"the postal prefix to resolve, matched exactly, e.g. 013"`
}

type regionIDInput struct {
	RegionID string `json:"regionId" jsonschema:"the region id, e.g. region-9c21"`
}

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
// (REST returns the regions as a bare array; the extra envelope key is the
// only shape difference between the two surfaces).
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
		Mode: l.Mode, NominalTransitMinutes: l.NominalTransitMinutes, Status: l.Status, ClosedAt: instant(l.ClosedAt),
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

// --- handlers -----------------------------------------------------------------

func (d Deps) getNode(ctx context.Context, in nodeIDInput) (nodeView, error) {
	n, err := d.GetNode.Execute(ctx, node.ID(in.NodeID))
	if err != nil {
		return nodeView{}, mapError(err)
	}
	return toNode(n), nil
}

func (d Deps) listNodes(ctx context.Context, in listNodesInput) (nodeListView, error) {
	nodes, err := d.ListNodes.Execute(ctx, usecases.ListNodesQuery{
		Type: node.Type(in.Type), RegionID: region.ID(in.RegionID), Status: node.Status(in.Status),
	})
	if err != nil {
		return nodeListView{}, mapError(err)
	}
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toNode(n))
	}
	return nodeListView{Nodes: out}, nil
}

func (d Deps) listLanes(ctx context.Context, in listLanesInput) (laneListView, error) {
	lanes, err := d.ListLanes.Execute(ctx, usecases.ListLanesQuery{
		Origin: node.ID(in.Origin), Destination: node.ID(in.Destination),
	})
	if err != nil {
		return laneListView{}, mapError(err)
	}
	out := make([]laneView, 0, len(lanes))
	for _, l := range lanes {
		out = append(out, toLane(l))
	}
	return laneListView{Lanes: out}, nil
}

func (d Deps) resolveServiceArea(ctx context.Context, in postalPrefixInput) (serviceAreaView, error) {
	area, err := d.ResolveServiceArea.Execute(ctx, servicearea.PostalPrefix(in.PostalPrefix))
	if err != nil {
		return serviceAreaView{}, mapError(err)
	}
	return toServiceArea(area), nil
}

func (d Deps) getRegion(ctx context.Context, in regionIDInput) (regionView, error) {
	r, err := d.GetRegion.Execute(ctx, region.ID(in.RegionID))
	if err != nil {
		return regionView{}, mapError(err)
	}
	return toRegion(r), nil
}

func (d Deps) listRegions(ctx context.Context, _ struct{}) (regionsListView, error) {
	regions, err := d.ListRegions.Execute(ctx)
	if err != nil {
		return regionsListView{}, mapError(err)
	}
	out := make([]regionView, 0, len(regions))
	for _, r := range regions {
		out = append(out, toRegion(r))
	}
	return regionsListView{Regions: out}, nil
}

// --- registry -----------------------------------------------------------------

// registerTools adds the six read-only tools, one per read GET of
// apis/openapi.yaml. Every tool is annotated read-only, idempotent and
// closed-world; TestToolSurface pins the set and fails the build on any
// write-verb name (docs/adr/0008).
func (d Deps) registerTools(server *mcp.Server) {
	closedWorld := false
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld}

	addTool(server, &mcp.Tool{
		Name: "get_node",
		Description: "Read one network node with its current operational profile: type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, " +
			"RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB), subtype, operator, vendor's own code, region, timezone, " +
			"location, derived capabilities, status (PROPOSED, ACTIVE, SUSPENDED or RETIRED) and the profile revision the node " +
			"currently holds (operating hours, holidays, handling time, cutoffs, throughput cap, sort dwell). Read-only; an unknown " +
			"id fails with not-found.",
		Annotations: readOnly,
	}, d.getNode)

	addTool(server, &mcp.Tool{
		Name: "list_nodes",
		Description: "List the network's nodes by id ascending, optionally filtered by type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, " +
			"RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB), region and/or status (PROPOSED, ACTIVE, SUSPENDED or " +
			"RETIRED); the filters combine with AND and no match is an empty list. Each node carries its current operational profile. " +
			"Read-only; a type or status that is not an enumeration value fails with validation-failed.",
		Annotations: readOnly,
	}, d.listNodes)

	addTool(server, &mcp.Tool{
		Name: "list_lanes",
		Description: "List the directed lanes between nodes by id ascending, optionally only those leaving one origin node and/or " +
			"arriving at one destination node (the filters combine with AND). Open and closed lanes are both listed, distinguished " +
			"by status (OPEN or CLOSED); mode is GROUND or AIR and nominalTransitMinutes is the planned transit. Read-only; no match " +
			"is an empty list.",
		Annotations: readOnly,
	}, d.listLanes)

	addTool(server, &mcp.Tool{
		Name: "resolve_service_area",
		Description: "Resolve which delivery station serves a postal prefix: the prefix (matched exactly), the deliveryStationNodeId, " +
			"the station's regionId and the assignmentVersion. Read-only; an unassigned prefix fails with not-found.",
		Annotations: readOnly,
	}, d.resolveServiceArea)

	addTool(server, &mcp.Tool{
		Name: "get_region",
		Description: "Read one region of the network by its id: the code and the name it was defined with. Read-only; an unknown id " +
			"fails with not-found.",
		Annotations: readOnly,
	}, d.getRegion)

	addTool(server, &mcp.Tool{
		Name: "list_regions",
		Description: "List every region of the network by id ascending (code and name). Regions are few and are not paginated; none " +
			"defined is an empty list. Read-only.",
		Annotations: readOnly,
	}, d.listRegions)
}

// addTool registers one tool. A handler error is returned to the SDK as the
// handler's error, which the SDK turns into an isError tool result (never a
// transport/protocol failure), carrying the error text.
func addTool[In, Out any](
	server *mcp.Server,
	tool *mcp.Tool,
	handle func(context.Context, In) (Out, error),
) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := handle(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, err
		}
		return nil, out, nil
	})
}
