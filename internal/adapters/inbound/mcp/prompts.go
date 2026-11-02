package mcp

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveDeliveryCoverageSOP is the operator standard-operating-procedure the
// resolve_delivery_coverage prompt hands to the model (Agentic Standard v2,
// AS-3). Per the charter, prompts encode how to interpret results and what
// "done" means; this one names tools but NEVER calls them, and it cannot
// write: every tool it names is read-only (ADR 0008).
const resolveDeliveryCoverageSOP = `You are resolving delivery coverage for a postal prefix of the retail network. Use only the MCP tools of node-topology; never assume state — read it.

Procedure:
1. Call resolve_service_area(postalPrefix). The prefix is matched exactly (e.g. "013", not "01310"). The answer names the deliveryStationNodeId that serves it, its regionId and the assignmentVersion.
2. Call get_node(nodeId = that deliveryStationNodeId) to read the station itself: operator, region, timezone, location and its current operational profile (operating hours, holidays, handling time, cutoffs, throughput cap). A station that is not ACTIVE (PROPOSED, SUSPENDED or RETIRED) cannot actually serve the prefix today — say so.
3. If you need to know how volume reaches the station, call list_lanes(destination = the station's nodeId) and read the OPEN lanes (CLOSED ones are historical); nominalTransitMinutes is the planned transit and mode is GROUND or AIR. Regions, when you need the partition, are list_regions / get_region.

Interpretation:
- An unassigned prefix is an explicit not-found answer, not "no coverage": the network simply has no delivery station for it yet.
- assignmentVersion is the revision of the assignment: a higher version has replaced a lower one; there is exactly one assignment per prefix.
- Nothing in this server changes data: registering nodes, declaring profile revisions, lifecycle transitions, lanes, service areas and regions are REST operations of node-topology.

Done means: you have named the delivery station serving PREFIX (id, type, status), its region, whether it is currently ACTIVE, and — when asked — the OPEN lanes that reach it, each with its mode and planned transit.`

// registerPrompts adds the workflow prompt (the operator SOP of this
// context). Read-only by construction: a prompt returns text.
func (d Deps) registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "resolve_delivery_coverage",
		Description: "Standard operating procedure for resolving which delivery station serves a postal prefix, reading that station's operational profile, and tracing the OPEN lanes that reach it.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "postalPrefix",
			Description: "the postal prefix to resolve, matched exactly, e.g. 013",
			Required:    true,
		}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		prefix := strings.TrimSpace(req.Params.Arguments["postalPrefix"])
		if prefix == "" {
			prefix = "<postalPrefix>"
		}
		return &mcp.GetPromptResult{
			Description: "How to resolve delivery coverage: resolve the service area, read the station, trace the OPEN lanes to it.",
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: strings.ReplaceAll(resolveDeliveryCoverageSOP, "PREFIX", prefix)},
			}},
		}, nil
	})
}
