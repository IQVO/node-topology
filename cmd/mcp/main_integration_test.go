//go:build integration

package main

import (
	"context"
	"os"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	inboundmcp "github.com/claudioed/node-topology/internal/adapters/inbound/mcp"
	"github.com/claudioed/node-topology/internal/adapters/outbound/clock"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}

// The binary migrates a database itself (idempotently: pgtest's databases
// are already migrated, a second run must be a no-op), then reads through
// every MCP tool exactly what the write use cases persisted, and its reads
// add no outbox row.
func TestMCPAgainstPostgres_ReadsWhatTheAPIWroteAndNeverWrites(t *testing.T) {
	url := pgtest.URL(t) // a fresh, already migrated database
	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	countOutbox := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&n); err != nil {
			t.Fatalf("count outbox_events: %v", err)
		}
		return n
	}

	w := seedThroughWriteUseCases(t, url)
	before := countOutbox()
	if before < 4 {
		t.Fatalf("outbox rows after seeding = %d, want the region, node, profile and lane events", before)
	}

	deps, closeFn, err := buildDeps(ctx, quietLogger(), url, url)
	if err != nil {
		t.Fatalf("buildDeps: %v", err)
	}
	t.Cleanup(closeFn)

	ct, st := sdk.NewInMemoryTransports()
	ss, err := inboundmcp.NewServer(deps).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	session, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	assertSeededReads(t, session, w)

	if after := countOutbox(); after != before {
		t.Fatalf("outbox rows %d -> %d: the MCP binary wrote an event", before, after)
	}
}

// seeded is the shape of the world the write use cases persist.
type seeded struct {
	regionID, fcNodeID, scNodeID, dsNodeID, laneID string
}

// seedThroughWriteUseCases defines a region, registers and activates three
// nodes (declaring the fulfilment centre's profile first), opens a lane and
// assigns a service area, the way cmd/api does (same use cases, Postgres
// adapters, transactional outbox).
func seedThroughWriteUseCases(t *testing.T, url string) seeded {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	outbox := postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder())
	deps := usecases.Deps{
		Nodes: postgres.NewNodeRepo(pool), Lanes: postgres.NewLaneRepo(pool),
		ServiceAreas: postgres.NewServiceAreaRepo(pool), Regions: postgres.NewRegionRepo(pool),
		Outbox: outbox, UoW: postgres.NewUnitOfWork(pool), Clock: clock.System{}, IDs: ids.UUID{},
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	reg, err := usecases.NewDefineRegion(deps).Execute(ctx, usecases.DefineRegionCommand{Code: "NORTH", Name: "North"})
	must(err)

	fc, err := usecases.NewRegisterNode(deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.FulfillmentCenter, Subtype: node.SubtypeSortable, Operator: node.OperatorVendor,
		Vendor:   &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-CODE-1"},
		RegionID: region.ID(reg.RegionID), Timezone: "America/Sao_Paulo",
		Location: node.GeoLocation{PostalCode: "01310", CountryCode: "BR"},
	})
	must(err)
	_, err = usecases.NewDeclareOperationalProfile(deps).Execute(ctx, usecases.DeclareOperationalProfileCommand{
		NodeID: node.ID(fc.NodeID), Declaration: completeProfile(),
	})
	must(err)
	fc, err = usecases.NewActivateNode(deps).Execute(ctx, node.ID(fc.NodeID))
	must(err)

	sc, err := usecases.NewRegisterNode(deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.SortationCenter, Operator: node.OperatorNetwork, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01420", CountryCode: "BR"},
	})
	must(err)
	sc, err = usecases.NewActivateNode(deps).Execute(ctx, node.ID(sc.NodeID))
	must(err)

	ds, err := usecases.NewRegisterNode(deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.DeliveryStation, Operator: node.OperatorNetwork, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01550", CountryCode: "BR"},
	})
	must(err)
	ds, err = usecases.NewActivateNode(deps).Execute(ctx, node.ID(ds.NodeID))
	must(err)

	opened, err := usecases.NewOpenLane(deps).Execute(ctx, usecases.OpenLaneCommand{
		OriginNodeID: node.ID(sc.NodeID), DestinationNodeID: node.ID(ds.NodeID),
		Mode: lane.ModeGround, NominalTransitMinutes: 240,
	})
	must(err)

	_, err = usecases.NewAssignServiceArea(deps).Execute(ctx, usecases.AssignServiceAreaCommand{
		PostalPrefix: servicearea.PostalPrefix("013"), DeliveryStationNodeID: node.ID(ds.NodeID), AssignmentVersion: 1,
	})
	must(err)

	return seeded{regionID: reg.RegionID, fcNodeID: fc.NodeID, scNodeID: sc.NodeID, dsNodeID: ds.NodeID, laneID: opened.LaneID}
}

// completeProfile is a complete, valid revision 1 profile declaration (the
// same shape the repo-contract fixture uses).
func completeProfile() node.ProfileDeclaration {
	return node.ProfileDeclaration{
		Revision:            1,
		Timezone:            "America/Sao_Paulo",
		OperatingHours:      node.OperatingHours{node.Monday: {Start: "06:00", End: "22:00"}},
		HandlingTimeMinutes: 30,
		Cutoffs:             []node.CutoffSlot{{LocalTime: "14:00", Days: []node.Weekday{node.Monday}, MaxOrders: 10}},
		ThroughputCap:       node.ThroughputCap{Value: 100, TimeUnit: node.TimeUnitHour, Unit: node.UnitOrder},
		DeclaredBy:          node.DeclaredByNetworkOps,
	}
}

// assertSeededReads reads back, through every MCP tool, what
// seedThroughWriteUseCases persisted.
func assertSeededReads(t *testing.T, session *sdk.ClientSession, w seeded) {
	t.Helper()
	ctx := context.Background()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: err=%v res=%+v", name, err, res)
		}
		return res.StructuredContent.(map[string]any)
	}

	reg := call("get_region", map[string]any{"regionId": w.regionID})
	if reg["code"] != "NORTH" || reg["name"] != "North" {
		t.Fatalf("get_region = %v", reg)
	}
	if regions := call("list_regions", map[string]any{}); len(regions["regions"].([]any)) != 1 {
		t.Fatalf("list_regions = %v", regions)
	}

	fc := call("get_node", map[string]any{"nodeId": w.fcNodeID})
	if fc["nodeType"] != "FULFILLMENT_CENTER" || fc["status"] != "ACTIVE" {
		t.Fatalf("get_node = %v", fc)
	}
	profile, _ := fc["operationalProfile"].(map[string]any)
	if profile == nil || profile["profileRevision"] != 1.0 {
		t.Fatalf("get_node operationalProfile = %v", profile)
	}
	nodes := call("list_nodes", map[string]any{"type": "DELIVERY_STATION", "status": "ACTIVE"})["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["nodeId"] != w.dsNodeID {
		t.Fatalf("list_nodes = %v", nodes)
	}

	lanes := call("list_lanes", map[string]any{"origin": w.scNodeID})["lanes"].([]any)
	if len(lanes) != 1 {
		t.Fatalf("list_lanes = %v", lanes)
	}
	laneRow := lanes[0].(map[string]any)
	if laneRow["laneId"] != w.laneID || laneRow["destinationNodeId"] != w.dsNodeID || laneRow["mode"] != "GROUND" {
		t.Fatalf("list_lanes = %v", laneRow)
	}

	area := call("resolve_service_area", map[string]any{"postalPrefix": "013"})
	if area["deliveryStationNodeId"] != w.dsNodeID || area["regionId"] != w.regionID {
		t.Fatalf("resolve_service_area = %v", area)
	}
}
