//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"

	inbounda2a "github.com/claudioed/node-topology/internal/adapters/inbound/a2a"
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

// The binary migrates a database itself (idempotently: pgtest's databases are
// already migrated, a second run must be a no-op), then answers both skills,
// over the real JSON-RPC wire and the real router, with exactly what the write
// use cases persisted, and its reads add no outbox row: this read-only agent
// delegates nothing that could publish a fact.
func TestA2AAgainstPostgres_ReadsWhatTheAPIWroteAndNeverWrites(t *testing.T) {
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

	srv := httptest.NewServer(newRouter(inbounda2a.Handler(inbounda2a.NewExecutor(deps)), testCard(), quietLogger(), "node-topology-a2a"))
	t.Cleanup(srv.Close)
	client, err := a2aclient.NewFromCard(ctx, inbounda2a.Card(inbounda2a.Config{AgentURL: srv.URL + "/a2a", Version: "test"}))
	if err != nil {
		t.Fatal(err)
	}

	assertSeededSkills(t, client, w)

	if after := countOutbox(); after != before {
		t.Fatalf("outbox rows %d -> %d: the A2A binary wrote an event", before, after)
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

// answer sends one skill request and returns the envelope map of the reply.
func answer(t *testing.T, client *a2aclient.Client, skill string, args map[string]any) map[string]any {
	t.Helper()
	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewDataPart(map[string]any{"skill": skill, "arguments": args}))
	resp, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatalf("%s: %v", skill, err)
	}
	reply, ok := resp.(*a2a.Message)
	if !ok || len(reply.Parts) < 1 {
		t.Fatalf("%s: reply = %T", skill, resp)
	}
	data, _ := reply.Parts[0].Content.(a2a.Data)
	raw, _ := json.Marshal(data.Value)
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s: envelope %s: %v", skill, raw, err)
	}
	return env
}

// assertSeededSkills reads back, through both skills, what
// seedThroughWriteUseCases persisted.
func assertSeededSkills(t *testing.T, client *a2aclient.Client, w seeded) {
	t.Helper()

	env := answer(t, client, "serving_station", map[string]any{"postalPrefix": "013"})
	data, _ := env["data"].(map[string]any)
	st, _ := data["servingStation"].(map[string]any)
	if st == nil || st["deliveryStationNodeId"] != w.dsNodeID || st["regionId"] != w.regionID || st["assignmentVersion"] != 1.0 {
		t.Fatalf("serving_station = %v", env)
	}
	station, _ := st["station"].(map[string]any)
	if station["nodeId"] != w.dsNodeID || station["nodeType"] != "DELIVERY_STATION" || station["status"] != "ACTIVE" {
		t.Fatalf("serving_station station = %v", station)
	}

	env = answer(t, client, "find_nodes", map[string]any{"type": "FULFILLMENT_CENTER", "status": "ACTIVE"})
	list, _ := env["data"].(map[string]any)["nodeList"].(map[string]any)
	nodes, _ := list["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["nodeId"] != w.fcNodeID || nodes[0].(map[string]any)["profileRevision"] != 1.0 {
		t.Fatalf("find_nodes = %v", env)
	}
	if all := answer(t, client, "find_nodes", map[string]any{})["data"].(map[string]any)["nodeList"].(map[string]any)["count"]; all != 3.0 {
		t.Fatalf("find_nodes count = %v, want the three seeded nodes", all)
	}

	assertSeededProblems(t, client)
}

// assertSeededProblems checks that rejections are answered in-band with the
// REST status classification.
func assertSeededProblems(t *testing.T, client *a2aclient.Client) {
	t.Helper()
	for _, tc := range []struct {
		skill  string
		args   map[string]any
		status float64
		slug   string
	}{
		{"serving_station", map[string]any{"postalPrefix": "999"}, 404, "not-found"},
		{"find_nodes", map[string]any{"type": "WAREHOUSE"}, 422, "validation-failed"},
		{"find_nodes", map[string]any{"regionId": "a\x00b"}, 400, "invalid-argument"},
	} {
		p, _ := answer(t, client, tc.skill, tc.args)["problem"].(map[string]any)
		if p == nil || p["status"] != tc.status || p["type"] != tc.slug {
			t.Fatalf("%s %v: problem = %v, want %v %s", tc.skill, tc.args, p, tc.status, tc.slug)
		}
	}
}
