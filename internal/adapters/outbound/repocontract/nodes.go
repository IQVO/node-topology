package repocontract

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

var nodeCases = []named{
	{"save then get round-trips the snapshot with its profile", nodeRoundTrip},
	{"get of an unknown id is ErrNotFound", nodeGetMiss},
	{"a loaded node does not alias stored state", nodeNoAliasing},
	{"find by vendor reference hits and misses", nodeFindByVendorRef},
	{"a second node with the same vendor code is ErrVendorNodeCodeTaken", nodeVendorCodeTaken},
	{"the same code under another vendor is accepted", nodeSameCodeOtherVendor},
	{"update with the next version is stored", nodeUpdate},
	{"stale and phantom saves are ErrConcurrentUpdate", nodeConcurrency},
	{"every profile revision is its own row, oldest first", nodeProfileRevisions},
	{"inbound limits are kept per revision and absent ones stay absent", nodeInboundLimitsPerRevision},
	{"saving without a new revision adds no row", nodeNoRevisionNoRow},
	{"profile revisions of an unknown node are ErrNotFound", nodeProfileRevisionsMiss},
	{"list filters by type, region and status ordered by id", nodeList},
}

// seedRegion stores a region and returns it.
func seedRegion(t *testing.T, f Fixture, id region.ID, code string) *region.Region {
	t.Helper()
	r := NewRegion(t, id, code)
	noErr(t, f.Regions.Save(context.Background(), r))
	return r
}

// saveActive stores a node the way the service does, one saved version per
// command (register, declare a profile, activate), and returns it as stored.
func saveActive(t *testing.T, f Fixture, reg *region.Region, typ node.Type, id node.ID, code string) *node.Node {
	t.Helper()
	ctx := context.Background()
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, typ, id, code)))
	n, err := f.Nodes.Get(ctx, id)
	noErr(t, err)
	_, err = n.DeclareProfile(Declaration(1), Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	_, err = n.Activate(Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	got, err := f.Nodes.Get(ctx, id)
	noErr(t, err)
	return got
}

func nodeRoundTrip(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	n := saveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	got, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if !reflect.DeepEqual(got.Snapshot(), n.Snapshot()) || got.Status() != node.StatusActive || got.Version() != 3 {
		t.Fatalf("snapshot = %+v, want ACTIVE at version 3", got.Snapshot())
	}
	if p, ok := got.Profile(); !ok || p.Revision != 1 {
		t.Fatalf("profile = %+v ok=%v, want revision 1", p, ok)
	}
}

func nodeGetMiss(t *testing.T, f Fixture) {
	_, err := f.Nodes.Get(context.Background(), "nope")
	wantErr(t, err, ports.ErrNotFound)
}

func nodeNoAliasing(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.DeliveryStation, "node-1", "")))
	a, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	_, err = a.Activate(Now)
	noErr(t, err)
	b, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if b.Status() != node.StatusProposed {
		t.Fatalf("status = %s, want PROPOSED: an unsaved change leaked into the store", b.Status())
	}
}

func nodeFindByVendorRef(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.FulfillmentCenter, "node-1", "FC-1")))
	got, err := f.Nodes.FindByVendorRef(ctx, "vendor-1", "FC-1")
	noErr(t, err)
	if got.ID() != "node-1" {
		t.Fatalf("id = %s, want node-1", got.ID())
	}
	for _, miss := range [][2]string{{"vendor-1", "FC-2"}, {"vendor-2", "FC-1"}} {
		_, err = f.Nodes.FindByVendorRef(ctx, miss[0], miss[1])
		wantErr(t, err, ports.ErrNotFound)
	}
}

func nodeVendorCodeTaken(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.FulfillmentCenter, "node-1", "FC-1")))
	wantErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.FulfillmentCenter, "node-2", "FC-1")), node.ErrVendorNodeCodeTaken)
	_, err := f.Nodes.Get(ctx, "node-2")
	wantErr(t, err, ports.ErrNotFound)
}

func nodeSameCodeOtherVendor(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.FulfillmentCenter, "node-1", "FC-1")))
	other := NewNode(t, reg, node.FulfillmentCenter, "node-2", "FC-1")
	snap := other.Snapshot()
	snap.Vendor = &node.VendorRef{VendorID: "vendor-2", VendorNodeCode: "FC-1"}
	other, err := node.Restore(snap)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, other))
}

func nodeUpdate(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.DeliveryStation, "node-1", "")))
	n, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	_, err = n.Activate(Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	got, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if got.Status() != node.StatusActive || got.Version() != 2 {
		t.Fatalf("status=%s version=%d, want ACTIVE at 2", got.Status(), got.Version())
	}
}

func nodeConcurrency(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.DeliveryStation, "node-1", "")))
	winner, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	loser, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	_, err = winner.Activate(Now)
	noErr(t, err)
	_, err = loser.Activate(Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, winner))
	wantErr(t, f.Nodes.Save(ctx, loser), ports.ErrConcurrentUpdate)
	// creating an id that exists, and updating a row that was never stored
	wantErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.DeliveryStation, "node-1", "")), ports.ErrConcurrentUpdate)
	ghost := NewActiveNode(t, reg, node.DeliveryStation, "ghost", "")
	_, err = ghost.Suspend("audit", Now)
	noErr(t, err)
	wantErr(t, f.Nodes.Save(ctx, ghost), ports.ErrConcurrentUpdate)
}

func nodeProfileRevisions(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	n := saveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1")
	second := Declaration(2)
	second.HandlingTimeMinutes = 45
	_, err := n.DeclareProfile(second, Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))

	rows, err := f.Nodes.ProfileRevisions(ctx, "node-1")
	noErr(t, err)
	if len(rows) != 2 || rows[0].Revision != 1 || rows[1].Revision != 2 {
		t.Fatalf("revisions = %+v, want 1 then 2", rows)
	}
	if rows[0].HandlingTimeMinutes != 30 || rows[1].HandlingTimeMinutes != 45 {
		t.Fatalf("an earlier revision was edited: %+v", rows)
	}
	got, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if p, _ := got.Profile(); p.Revision != 2 || p.HandlingTimeMinutes != 45 {
		t.Fatalf("current profile = %+v, want revision 2", p)
	}
}

func nodeInboundLimitsPerRevision(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	n := saveActive(t, f, reg, node.FulfillmentCenter, "node-1", "FC-1") // revision 1: no inbound limits
	weight, yes, no := 25000, true, false
	second := Declaration(2)
	second.Inbound = &node.InboundLimits{
		MaxCartonWeightG: &weight, MaxCartonDimsMm: &node.CartonDims{L: 600, W: 400, H: 300}, AcceptsPallets: &yes, RequiresSscc: &no,
	}
	_, err := n.DeclareProfile(second, Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	third := Declaration(3)
	third.Inbound = &node.InboundLimits{RequiresSscc: &yes}
	_, err = n.DeclareProfile(third, Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	fourth := Declaration(4) // declares none again
	_, err = n.DeclareProfile(fourth, Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))

	rows, err := f.Nodes.ProfileRevisions(ctx, "node-1")
	noErr(t, err)
	if len(rows) != 4 || rows[0].Inbound != nil || rows[3].Inbound != nil {
		t.Fatalf("revisions 1 and 4 declare no inbound limits: %+v", rows)
	}
	if !reflect.DeepEqual(rows[1].Inbound, second.Inbound) || !reflect.DeepEqual(rows[2].Inbound, third.Inbound) {
		t.Fatalf("inbound limits changed in storage: rev 2 %+v, rev 3 %+v", rows[1].Inbound, rows[2].Inbound)
	}
	got, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	if p, _ := got.Profile(); p.Inbound != nil {
		t.Fatalf("the current revision declares no inbound limits: %+v", p.Inbound)
	}
}

func nodeNoRevisionNoRow(t *testing.T, f Fixture) {
	ctx := context.Background()
	reg := seedRegion(t, f, "region-1", "SP")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, reg, node.DeliveryStation, "node-1", "")))
	rows, err := f.Nodes.ProfileRevisions(ctx, "node-1")
	noErr(t, err)
	if len(rows) != 0 {
		t.Fatalf("revisions = %+v, want none", rows)
	}
	n, err := f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	_, err = n.DeclareProfile(Declaration(1), Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	n, err = f.Nodes.Get(ctx, "node-1")
	noErr(t, err)
	_, err = n.Activate(Now)
	noErr(t, err)
	noErr(t, f.Nodes.Save(ctx, n))
	rows, err = f.Nodes.ProfileRevisions(ctx, "node-1")
	noErr(t, err)
	if len(rows) != 1 {
		t.Fatalf("revisions = %d, want 1 (an update of the node must not add a row)", len(rows))
	}
}

func nodeProfileRevisionsMiss(t *testing.T, f Fixture) {
	_, err := f.Nodes.ProfileRevisions(context.Background(), "nope")
	wantErr(t, err, ports.ErrNotFound)
}

func nodeList(t *testing.T, f Fixture) {
	ctx := context.Background()
	sp := seedRegion(t, f, "region-1", "SP")
	rj := seedRegion(t, f, "region-2", "RJ")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, sp, node.DeliveryStation, "node-b", "")))
	noErr(t, f.Nodes.Save(ctx, NewNode(t, rj, node.DeliveryStation, "node-c", "")))
	saveActive(t, f, sp, node.DeliveryStation, "node-a", "")
	noErr(t, f.Nodes.Save(ctx, NewNode(t, sp, node.FulfillmentCenter, "node-d", "FC-1")))

	cases := []struct {
		name   string
		filter repository.NodeFilter
		want   []node.ID
	}{
		{"no filter", repository.NodeFilter{}, []node.ID{"node-a", "node-b", "node-c", "node-d"}},
		{"type", repository.NodeFilter{Type: node.DeliveryStation}, []node.ID{"node-a", "node-b", "node-c"}},
		{"region", repository.NodeFilter{RegionID: "region-2"}, []node.ID{"node-c"}},
		{"status", repository.NodeFilter{Status: node.StatusActive}, []node.ID{"node-a"}},
		{"all three", repository.NodeFilter{Type: node.DeliveryStation, RegionID: "region-1", Status: node.StatusProposed}, []node.ID{"node-b"}},
		{"no match", repository.NodeFilter{Type: node.AirHub}, []node.ID{}},
	}
	for _, c := range cases {
		got, err := f.Nodes.List(ctx, c.filter)
		noErr(t, err)
		ids := make([]node.ID, len(got))
		for i, n := range got {
			ids[i] = n.ID()
		}
		if !reflect.DeepEqual(ids, c.want) {
			t.Fatalf("%s: ids = %v, want %v", c.name, ids, c.want)
		}
	}
}
