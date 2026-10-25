package mcp_test

import (
	"testing"
)

func TestGetNodeReturnsTheNodeWithItsProfile(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_node", map[string]any{"nodeId": w.fcNodeID})
	assertNodeIdentity(t, got, w)
	assertNodeVendorAndLocation(t, got)
	assertNodeCapabilities(t, got)
	assertNodeProfile(t, got)
}

// assertNodeIdentity checks the node's own scalars.
func assertNodeIdentity(t *testing.T, got map[string]any, w world) {
	t.Helper()
	for k, v := range map[string]any{
		"nodeId": w.fcNodeID, "nodeType": "FULFILLMENT_CENTER", "subtype": "SORTABLE", "operator": "VENDOR",
		"regionId": w.regionID, "timezone": "America/Sao_Paulo", "status": "ACTIVE",
	} {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if _, ok := got["suspendReason"]; ok {
		t.Fatalf("suspendReason must be absent on an ACTIVE node: %v", got)
	}
}

// assertNodeVendorAndLocation checks the vendor's own code and the location.
func assertNodeVendorAndLocation(t *testing.T, got map[string]any) {
	t.Helper()
	vendor, _ := got["vendorRef"].(map[string]any)
	if vendor["vendorId"] != "vendor-1" || vendor["vendorNodeCode"] != "FC-CODE-1" {
		t.Fatalf("vendorRef = %v", vendor)
	}
	loc, _ := got["location"].(map[string]any)
	if loc["postalCode"] != "01310" || loc["countryCode"] != "BR" {
		t.Fatalf("location = %v", loc)
	}
}

// assertNodeCapabilities checks the type-derived capabilities.
func assertNodeCapabilities(t *testing.T, got map[string]any) {
	t.Helper()
	caps, _ := got["capabilities"].(map[string]any)
	if caps["outbound"] != true || caps["lastMile"] != false {
		t.Fatalf("capabilities = %v", caps)
	}
}

// assertNodeProfile checks the declared operational profile revision.
func assertNodeProfile(t *testing.T, got map[string]any) {
	t.Helper()
	profile, _ := got["operationalProfile"].(map[string]any)
	if profile["profileRevision"] != 1.0 || profile["handlingTimeMinutes"] != 30.0 || profile["declaredBy"] != "NETWORK_OPS" {
		t.Fatalf("operationalProfile = %v", profile)
	}
	hours, _ := profile["operatingHoursByDay"].(map[string]any)
	monday, _ := hours["monday"].(map[string]any)
	if monday["start"] != "06:00" || monday["end"] != "22:00" {
		t.Fatalf("operatingHoursByDay = %v", hours)
	}
	cutoffs, _ := profile["cutoffs"].([]any)
	if len(cutoffs) != 1 || cutoffs[0].(map[string]any)["localTime"] != "14:00" {
		t.Fatalf("cutoffs = %v", cutoffs)
	}
	if _, ok := got["retiredAt"]; ok {
		t.Fatalf("retiredAt must be absent on an ACTIVE node: %v", got)
	}
}

func TestGetNodeErrors(t *testing.T) {
	h, _ := withWorld(t)
	failWith(t, h.session, "get_node", map[string]any{"nodeId": "node-nope"}, "not-found")
}

func TestListNodesFiltersAndOrders(t *testing.T) {
	h, w := withWorld(t)

	all := h.ok(t, "list_nodes", map[string]any{})
	nodes := all["nodes"].([]any)
	if len(nodes) != 3 {
		t.Fatalf("list_nodes = %d nodes, want 3: %v", len(nodes), all)
	}
	byType := h.ok(t, "list_nodes", map[string]any{"type": "DELIVERY_STATION"})
	if ids := idsOf(byType); len(ids) != 1 || ids[0] != w.dsNodeID {
		t.Fatalf("type filter = %v, want [%s]", ids, w.dsNodeID)
	}
	byRegion := h.ok(t, "list_nodes", map[string]any{"regionId": w.regionID})
	if len(idsOf(byRegion)) != 3 {
		t.Fatalf("region filter = %v", byRegion)
	}
	byStatus := h.ok(t, "list_nodes", map[string]any{"status": "ACTIVE"})
	if len(idsOf(byStatus)) != 3 {
		t.Fatalf("status filter = %v", byStatus)
	}
	combined := h.ok(t, "list_nodes", map[string]any{"type": "FULFILLMENT_CENTER", "status": "ACTIVE"})
	if ids := idsOf(combined); len(ids) != 1 || ids[0] != w.fcNodeID {
		t.Fatalf("combined filters = %v", combined)
	}
	none := h.ok(t, "list_nodes", map[string]any{"type": "AIR_HUB"})
	if ids := idsOf(none); len(ids) != 0 {
		t.Fatalf("no match must be an empty list: %v", none)
	}
}

func TestListNodesErrors(t *testing.T) {
	h, _ := withWorld(t)
	failWith(t, h.session, "list_nodes", map[string]any{"type": "BOGUS"}, "validation-failed")
	failWith(t, h.session, "list_nodes", map[string]any{"status": "LOST"}, "validation-failed")
}

func TestListLanesFilters(t *testing.T) {
	h, w := withWorld(t)

	all := h.ok(t, "list_lanes", map[string]any{})
	lanes := all["lanes"].([]any)
	if len(lanes) != 1 {
		t.Fatalf("list_lanes = %d lanes, want 1: %v", len(lanes), all)
	}
	first := lanes[0].(map[string]any)
	if first["laneId"] != w.laneID || first["originNodeId"] != w.scNodeID ||
		first["destinationNodeId"] != w.dsNodeID || first["mode"] != "GROUND" ||
		first["nominalTransitMinutes"] != 240.0 || first["status"] != "OPEN" {
		t.Fatalf("lane = %v", first)
	}
	byOrigin := h.ok(t, "list_lanes", map[string]any{"origin": w.scNodeID})
	if got := byOrigin["lanes"].([]any); len(got) != 1 {
		t.Fatalf("origin filter = %v", byOrigin)
	}
	byDestination := h.ok(t, "list_lanes", map[string]any{"destination": w.fcNodeID})
	if got := byDestination["lanes"].([]any); len(got) != 0 {
		t.Fatalf("destination filter = %v", byDestination)
	}
	both := h.ok(t, "list_lanes", map[string]any{"origin": w.scNodeID, "destination": w.dsNodeID})
	if got := both["lanes"].([]any); len(got) != 1 {
		t.Fatalf("combined filters = %v", both)
	}
}

func TestResolveServiceArea(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "resolve_service_area", map[string]any{"postalPrefix": "013"})
	if got["postalPrefix"] != "013" || got["deliveryStationNodeId"] != w.dsNodeID ||
		got["regionId"] != w.regionID || got["assignmentVersion"] != 1.0 {
		t.Fatalf("resolve_service_area = %v", got)
	}
}

func TestResolveServiceAreaErrors(t *testing.T) {
	h, _ := withWorld(t)
	failWith(t, h.session, "resolve_service_area", map[string]any{"postalPrefix": "999"}, "not-found")
}

func TestGetRegion(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "get_region", map[string]any{"regionId": w.regionID})
	if got["regionId"] != w.regionID || got["code"] != "NORTH" || got["name"] != "North" {
		t.Fatalf("get_region = %v", got)
	}
}

func TestGetRegionErrors(t *testing.T) {
	h, _ := withWorld(t)
	failWith(t, h.session, "get_region", map[string]any{"regionId": "region-nope"}, "not-found")
}

func TestListRegions(t *testing.T) {
	h, w := withWorld(t)
	got := h.ok(t, "list_regions", map[string]any{})
	if ids := regionIDs(t, got); len(ids) != 1 || ids[0] != w.regionID {
		t.Fatalf("list_regions = %v", got)
	}
}

// idsOf pulls the node ids out of a list_nodes result.
func idsOf(page map[string]any) []string {
	nodes, _ := page["nodes"].([]any)
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.(map[string]any)["nodeId"].(string))
	}
	return out
}

// regionIDs pulls the region ids out of a list_regions result.
func regionIDs(t *testing.T, page map[string]any) []string {
	t.Helper()
	regions, ok := page["regions"].([]any)
	if !ok {
		t.Fatalf("list_regions = %#v, want a {regions: [...]} object", page)
	}
	out := make([]string, 0, len(regions))
	for _, r := range regions {
		out = append(out, r.(map[string]any)["regionId"].(string))
	}
	return out
}

func TestReadsEmitNothing(t *testing.T) {
	h, w := withWorld(t)
	before := len(h.store.Outbox().Events())
	h.ok(t, "get_node", map[string]any{"nodeId": w.fcNodeID})
	h.ok(t, "list_nodes", map[string]any{})
	h.ok(t, "list_lanes", map[string]any{})
	h.ok(t, "resolve_service_area", map[string]any{"postalPrefix": "013"})
	h.ok(t, "get_region", map[string]any{"regionId": w.regionID})
	h.ok(t, "list_regions", map[string]any{})
	if after := len(h.store.Outbox().Events()); after != before {
		t.Fatalf("events %d -> %d: a read tool emitted an event", before, after)
	}
}
