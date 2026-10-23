package usecases_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

func TestGetNodeReturnsTheNodeWithItsProfile(t *testing.T) {
	e := newEnv(t)
	e.seedNode(e.seedRegion("r-1", "SP"), node.FulfillmentCenter, "n-1", "FC-1", node.StatusActive)

	got, err := usecases.NewGetNode(e.nodes).Execute(context.Background(), "n-1")
	wantErr(t, err, nil)

	if got.NodeID != "n-1" || got.Status != "ACTIVE" || got.OperationalProfile == nil || got.OperationalProfile.ProfileRevision != 1 {
		t.Fatalf("view = %+v", got)
	}
}

func TestGetNodeFailures(t *testing.T) {
	e := newEnv(t)
	_, err := usecases.NewGetNode(e.nodes).Execute(context.Background(), "ghost")
	wantErr(t, err, ports.ErrNotFound)

	e.nodes.getErr = errBoom
	_, err = usecases.NewGetNode(e.nodes).Execute(context.Background(), "ghost")
	wantErr(t, err, errBoom)
}

func servicePrefix(s string) servicearea.PostalPrefix { return servicearea.PostalPrefix(s) }

func TestListNodesFiltersAndOrders(t *testing.T) {
	e := newEnv(t)
	sp, rj := e.seedRegion("r-1", "SP"), e.seedRegion("r-2", "RJ")
	e.seedNode(sp, node.DeliveryStation, "n-b", "", node.StatusProposed)
	e.seedNode(rj, node.DeliveryStation, "n-c", "", node.StatusActive)
	e.seedNode(sp, node.DeliveryStation, "n-a", "", node.StatusActive)
	e.seedNode(sp, node.FulfillmentCenter, "n-d", "FC-1", node.StatusProposed)

	cases := []struct {
		name  string
		query usecases.ListNodesQuery
		want  []string
	}{
		{"all", usecases.ListNodesQuery{}, []string{"n-a", "n-b", "n-c", "n-d"}},
		{"by type", usecases.ListNodesQuery{Type: node.DeliveryStation}, []string{"n-a", "n-b", "n-c"}},
		{"by region", usecases.ListNodesQuery{RegionID: "r-2"}, []string{"n-c"}},
		{"by status", usecases.ListNodesQuery{Status: node.StatusProposed}, []string{"n-b", "n-d"}},
		{"combined with AND", usecases.ListNodesQuery{Type: node.DeliveryStation, RegionID: "r-1", Status: node.StatusActive}, []string{"n-a"}},
		{"none", usecases.ListNodesQuery{Type: node.AirHub}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := usecases.NewListNodes(e.nodes).Execute(context.Background(), c.query)
			wantErr(t, err, nil)
			if got == nil {
				t.Fatal("an empty result must be an empty list, not nil")
			}
			ids := []string{}
			for _, v := range got {
				ids = append(ids, v.NodeID)
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("ids = %v, want %v", ids, c.want)
			}
		})
	}
}

func TestListNodesFailures(t *testing.T) {
	e := newEnv(t)
	list := usecases.NewListNodes(e.nodes)
	_, err := list.Execute(context.Background(), usecases.ListNodesQuery{Type: "WAREHOUSE"})
	wantErr(t, err, shared.ErrValidation)
	_, err = list.Execute(context.Background(), usecases.ListNodesQuery{Status: "BROKEN"})
	wantErr(t, err, shared.ErrValidation)
	e.nodes.listErr = errBoom
	_, err = list.Execute(context.Background(), usecases.ListNodesQuery{})
	wantErr(t, err, errBoom)
}

func TestListLanesFiltersAndOrders(t *testing.T) {
	e := newEnv(t)
	e.seedLaneEnds()
	e.seedLane("lane-b", "n-o", "n-d", lane.ModeAir)
	e.seedLane("lane-a", "n-o", "n-d", lane.ModeGround)
	if _, err := usecases.NewCloseLane(e.deps).Execute(context.Background(), "lane-b"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		query usecases.ListLanesQuery
		want  []string
	}{
		{"all, open and closed", usecases.ListLanesQuery{}, []string{"lane-a", "lane-b"}},
		{"by origin", usecases.ListLanesQuery{Origin: "n-o"}, []string{"lane-a", "lane-b"}},
		{"by destination", usecases.ListLanesQuery{Destination: "n-d"}, []string{"lane-a", "lane-b"}},
		{"both", usecases.ListLanesQuery{Origin: "n-o", Destination: "n-d"}, []string{"lane-a", "lane-b"}},
		{"none", usecases.ListLanesQuery{Origin: "n-d"}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := usecases.NewListLanes(e.lanes).Execute(context.Background(), c.query)
			wantErr(t, err, nil)
			ids := []string{}
			for _, v := range got {
				ids = append(ids, v.LaneID)
			}
			if !reflect.DeepEqual(ids, c.want) {
				t.Fatalf("ids = %v, want %v", ids, c.want)
			}
		})
	}
	closed, _ := usecases.NewListLanes(e.lanes).Execute(context.Background(), usecases.ListLanesQuery{})
	if closed[0].Status != "OPEN" || closed[1].Status != "CLOSED" || closed[1].ClosedAt == nil {
		t.Fatalf("views = %+v", closed)
	}

	e.lanes.listErr = errBoom
	_, err := usecases.NewListLanes(e.lanes).Execute(context.Background(), usecases.ListLanesQuery{})
	wantErr(t, err, errBoom)
}

func TestResolveServiceArea(t *testing.T) {
	e := newEnv(t)
	reg := e.seedRegion("r-1", "SP")
	e.seedNode(reg, node.DeliveryStation, "n-s", "", node.StatusActive)
	e.seedArea("013", "n-s", reg, 4)
	uc := usecases.NewResolveServiceArea(e.areas)

	got, err := uc.Execute(context.Background(), "013")
	wantErr(t, err, nil)
	if got.PostalPrefix != "013" || got.DeliveryStationNodeID != "n-s" || got.RegionID != "r-1" || got.AssignmentVersion != 4 {
		t.Fatalf("view = %+v", got)
	}
	for _, prefix := range []string{"01", "0130", "ghost"} {
		_, err = uc.Execute(context.Background(), servicePrefix(prefix))
		wantErr(t, err, ports.ErrNotFound)
	}
	e.areas.getErr = errBoom
	_, err = uc.Execute(context.Background(), "013")
	wantErr(t, err, errBoom)
}
