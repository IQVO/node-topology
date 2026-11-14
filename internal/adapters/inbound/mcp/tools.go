package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/node-topology/internal/application/usecases"
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

// --- handlers -----------------------------------------------------------------

func (d Deps) getNode(ctx context.Context, in nodeIDInput) (nodeView, error) {
	n, err := d.GetNode.Execute(ctx, node.ID(in.NodeID))
	if err != nil {
		return nodeView{}, mapError(ctx, err)
	}
	return toNode(n), nil
}

func (d Deps) listNodes(ctx context.Context, in listNodesInput) (nodeListView, error) {
	nodes, err := d.ListNodes.Execute(ctx, usecases.ListNodesQuery{
		Type: node.Type(in.Type), RegionID: region.ID(in.RegionID), Status: node.Status(in.Status),
	})
	if err != nil {
		return nodeListView{}, mapError(ctx, err)
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
		return laneListView{}, mapError(ctx, err)
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
		return serviceAreaView{}, mapError(ctx, err)
	}
	return toServiceArea(area), nil
}

func (d Deps) getRegion(ctx context.Context, in regionIDInput) (regionView, error) {
	r, err := d.GetRegion.Execute(ctx, region.ID(in.RegionID))
	if err != nil {
		return regionView{}, mapError(ctx, err)
	}
	return toRegion(r), nil
}

func (d Deps) listRegions(ctx context.Context, _ struct{}) (regionsListView, error) {
	regions, err := d.ListRegions.Execute(ctx, usecases.ListRegionsQuery{})
	if err != nil {
		return regionsListView{}, mapError(ctx, err)
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

	addTool(server, &mcp.Tool{
		Name: "get_node",
		Description: "Read one network node with its current operational profile: type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, " +
			"RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB), subtype, operator, vendor's own code, region, timezone, " +
			"location, derived capabilities, status (PROPOSED, ACTIVE, SUSPENDED or RETIRED) and the profile revision the node " +
			"currently holds (operating hours, holidays, handling time, cutoffs, throughput cap, sort dwell). Read-only; an unknown " +
			"id fails with not-found.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.getNode)

	addTool(server, &mcp.Tool{
		Name: "list_nodes",
		Description: "List the network's nodes by id ascending, optionally filtered by type (FULFILLMENT_CENTER, INBOUND_CROSS_DOCK, " +
			"RETURNS_CENTER, SORTATION_CENTER, DELIVERY_STATION or AIR_HUB), region and/or status (PROPOSED, ACTIVE, SUSPENDED or " +
			"RETIRED); the filters combine with AND and no match is an empty list. Each node carries its current operational profile. " +
			"Read-only; a type or status that is not an enumeration value fails with validation-failed.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.listNodes)

	addTool(server, &mcp.Tool{
		Name: "list_lanes",
		Description: "List the directed lanes between nodes by id ascending, optionally only those leaving one origin node and/or " +
			"arriving at one destination node (the filters combine with AND). Open and closed lanes are both listed, distinguished " +
			"by status (OPEN or CLOSED); mode is GROUND or AIR and nominalTransitMinutes is the planned transit. Each lane carries " +
			"costPerUnitCents, the planner's declared marginal cost of moving one package-unit across it on one departure " +
			"(integer cents; 0 means nothing declared yet). Read-only; no match is an empty list.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.listLanes)

	addTool(server, &mcp.Tool{
		Name: "resolve_service_area",
		Description: "Resolve which delivery station serves a postal prefix: the prefix (matched exactly), the deliveryStationNodeId, " +
			"the station's regionId, the assignmentVersion, and — when the area's last mile is a carrier handoff — the deliveryMode " +
			"(CARRIER_HANDOFF) and the declared carrier options (carrierId, handoffNodeId, costPerPackageCents, transitMinutes); an " +
			"area served by the network's own route carries no mode or options. Read-only; an unassigned prefix fails with not-found.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.resolveServiceArea)

	addTool(server, &mcp.Tool{
		Name: "get_region",
		Description: "Read one region of the network by its id: the code and the name it was defined with. Read-only; an unknown id " +
			"fails with not-found.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
	}, d.getRegion)

	addTool(server, &mcp.Tool{
		Name: "list_regions",
		Description: "List every region of the network by id ascending (code and name). Regions are few and are not paginated; none " +
			"defined is an empty list. Read-only.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closedWorld},
		Meta:        authorityMeta(authorityL1),
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
