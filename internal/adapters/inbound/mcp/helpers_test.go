package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/claudioed/node-topology/internal/adapters/inbound/mcp"
	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// now is the fixture's fixed "now".
var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// world is the seeded topology every tool test reads: one region, three
// ACTIVE nodes (a vendor-run fulfillment centre with a declared profile, a
// network sortation centre and a network delivery station), one GROUND lane
// from the sortation centre to the delivery station, and postal prefix 013
// assigned to the delivery station.
type world struct {
	fcNodeID, scNodeID, dsNodeID, laneID, regionID string
}

// harness is a connected in-memory MCP client over the real server wired to
// in-memory repositories, exactly as cmd/mcp wires them (minus Postgres). The
// write use cases exist ONLY here, to seed data the way the REST adapter
// would: the MCP surface itself has none.
type harness struct {
	session *sdk.ClientSession
	store   *memory.Store
	deps    usecases.Deps
	clock   *memory.Clock
	ids     *memory.SequentialIDs
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := memory.NewStore()
	clock := memory.NewClock(now)
	ids := &memory.SequentialIDs{}
	deps := usecases.Deps{
		Nodes: store.Nodes(), Lanes: store.Lanes(), ServiceAreas: store.ServiceAreas(), Regions: store.Regions(),
		Outbox: store.Outbox(), UoW: store.UnitOfWork(), Clock: clock, IDs: ids,
	}
	return &harness{
		session: connectSession(t, inboundmcp.Deps{
			GetNode:            usecases.NewGetNode(deps.Nodes),
			ListNodes:          usecases.NewListNodes(deps.Nodes),
			ListLanes:          usecases.NewListLanes(deps.Lanes),
			ResolveServiceArea: usecases.NewResolveServiceArea(deps.ServiceAreas),
			GetRegion:          usecases.NewGetRegion(deps.Regions),
			ListRegions:        usecases.NewListRegions(deps.Regions),
		}),
		store: store, deps: deps, clock: clock, ids: ids,
	}
}

func connectSession(t *testing.T, deps inboundmcp.Deps) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	serverSession, err := inboundmcp.NewServer(deps).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// seedWorld populates the fixture the way the REST adapter's write use cases
// would (define a region, register and activate the nodes, declare the
// fulfilment centre's profile, open a lane, assign a service area).
func (h *harness) seedWorld(t *testing.T) world {
	t.Helper()
	ctx := t.Context()

	reg, err := usecases.NewDefineRegion(h.deps).Execute(ctx, usecases.DefineRegionCommand{Code: "NORTH", Name: "North"})
	must(t, err)

	fc, err := usecases.NewRegisterNode(h.deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.FulfillmentCenter, Subtype: node.SubtypeSortable, Operator: node.OperatorVendor,
		Vendor: &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-CODE-1"}, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	})
	must(t, err)
	_, err = usecases.NewDeclareOperationalProfile(h.deps).Execute(ctx, usecases.DeclareOperationalProfileCommand{
		NodeID: node.ID(fc.NodeID), Declaration: profileDeclaration(),
	})
	must(t, err)
	fc, err = usecases.NewActivateNode(h.deps).Execute(ctx, node.ID(fc.NodeID))
	must(t, err)

	sc, err := usecases.NewRegisterNode(h.deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.SortationCenter, Operator: node.OperatorNetwork, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01420", CountryCode: "BR"},
	})
	must(t, err)
	sc, err = usecases.NewActivateNode(h.deps).Execute(ctx, node.ID(sc.NodeID))
	must(t, err)

	ds, err := usecases.NewRegisterNode(h.deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.DeliveryStation, Operator: node.OperatorNetwork, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01550", CountryCode: "BR"},
	})
	must(t, err)
	ds, err = usecases.NewActivateNode(h.deps).Execute(ctx, node.ID(ds.NodeID))
	must(t, err)

	opened, err := usecases.NewOpenLane(h.deps).Execute(ctx, usecases.OpenLaneCommand{
		OriginNodeID: node.ID(sc.NodeID), DestinationNodeID: node.ID(ds.NodeID), Mode: lane.ModeGround, NominalTransitMinutes: 240,
	})
	must(t, err)

	_, err = usecases.NewAssignServiceArea(h.deps).Execute(ctx, usecases.AssignServiceAreaCommand{
		PostalPrefix: servicearea.PostalPrefix("013"), DeliveryStationNodeID: node.ID(ds.NodeID), AssignmentVersion: 1,
	})
	must(t, err)

	return world{fcNodeID: fc.NodeID, scNodeID: sc.NodeID, dsNodeID: ds.NodeID, laneID: opened.LaneID, regionID: reg.RegionID}
}

// profileDeclaration is a complete, valid revision 1 declaration (the same
// shape the repo-contract fixture uses).
func profileDeclaration() node.ProfileDeclaration {
	return repocontract.Declaration(1)
}

// withWorld is a harness whose world is already seeded.
func withWorld(t *testing.T) (*harness, world) {
	t.Helper()
	h := newHarness(t)
	return h, h.seedWorld(t)
}

// call invokes a tool and fails the test on a TRANSPORT/protocol error;
// tool-level failures come back as res.IsError.
func call(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: transport/protocol error (want a tool result): %v", name, err)
	}
	return res
}

// ok calls a tool, requires success, and returns its structured content.
func (h *harness) ok(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	res := call(t, h.session, name, args)
	if res.IsError {
		t.Fatalf("%s returned a tool error: %s", name, text(res))
	}
	sc, isMap := res.StructuredContent.(map[string]any)
	if !isMap {
		t.Fatalf("%s: structured content = %#v, want an object", name, res.StructuredContent)
	}
	return sc
}

// failWith calls a tool and requires an isError result whose text starts
// with the slug want.
func failWith(t *testing.T, session *sdk.ClientSession, name string, args map[string]any, want string) {
	t.Helper()
	res := call(t, session, name, args)
	if !res.IsError {
		t.Fatalf("%s: expected an isError tool result, got %#v", name, res.StructuredContent)
	}
	if got := text(res); !strings.HasPrefix(got, want+": ") {
		t.Fatalf("%s: error text %q does not start with %q", name, got, want+": ")
	}
}

func text(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, isText := c.(*sdk.TextContent); isText {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
