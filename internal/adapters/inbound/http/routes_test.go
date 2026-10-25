package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/claudioed/node-topology/internal/domain/lane"
)

const profileJSON = `{"profileRevision":1,"timezone":"America/Sao_Paulo",
	"operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"}},"holidays":["2026-12-25"],"handlingTimeMinutes":30,
	"cutoffs":[{"localTime":"14:00","days":["monday"],"maxOrders":10}],
	"throughputCap":{"value":100,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"NETWORK_OPS"%s}`

func profileWith(extra string) string { return sprintf(profileJSON, extra) }

func revision2(body string) string {
	return strings.Replace(body, `"profileRevision":1`, `"profileRevision":2`, 1)
}

func registerBody(nodeType, operator, extra string) string {
	return `{"nodeType":"` + nodeType + `","operator":"` + operator + `","regionId":"region-1","timezone":"America/Sao_Paulo",` +
		`"location":{"postalCode":"01310-100","countryCode":"BR","lat":-23.56,"lon":-46.65}` + extra + `}`
}

// world registers a region and two ACTIVE nodes forming an allowed lane pair,
// plus an ACTIVE delivery station, all through the REST API.
type world struct {
	a                     *api
	origin, dest, station string
}

func newWorld(t *testing.T) world {
	t.Helper()
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	if r := a.keyed(http.MethodPost, "/regions", `{"code":"SE","name":"Southeast"}`); r.status != http.StatusCreated {
		t.Fatalf("define region: %d %s", r.status, r.raw)
	}
	pair := lane.AllowedPairs()[0]
	w := world{a: a}
	w.origin = w.activeNode(t, string(pair.Origin), "c-origin")
	w.dest = w.activeNode(t, string(pair.Destination), "c-dest")
	w.station = w.activeNode(t, "DELIVERY_STATION", "c-station")
	return w
}

// activeNode registers, declares a profile for and activates a node.
func (w world) activeNode(t *testing.T, nodeType, code string) string {
	t.Helper()
	body := registerBody(nodeType, "NETWORK", "")
	if nodeType == "FULFILLMENT_CENTER" || nodeType == "INBOUND_CROSS_DOCK" || nodeType == "RETURNS_CENTER" {
		body = registerBody(nodeType, "VENDOR", `,"vendorRef":{"vendorId":"vendor-1","vendorNodeCode":"`+code+`"}`)
	}
	reg := w.a.keyed(http.MethodPost, "/nodes", body)
	if reg.status != http.StatusCreated {
		t.Fatalf("register %s: %d %s", nodeType, reg.status, reg.raw)
	}
	id := reg.field("nodeId")
	extra := ""
	if nodeType == "SORTATION_CENTER" || nodeType == "AIR_HUB" {
		extra = `,"sortDwellMinutes":20`
	}
	if r := w.a.keyed(http.MethodPut, "/nodes/"+id+"/operational-profile", profileWith(extra)); r.status != http.StatusOK {
		t.Fatalf("declare profile of %s: %d %s", nodeType, r.status, r.raw)
	}
	if r := w.a.keyed(http.MethodPost, "/nodes/"+id+":activate", ""); r.status != http.StatusOK {
		t.Fatalf("activate %s: %d %s", nodeType, r.status, r.raw)
	}
	return id
}

func TestNodeReadRoutes(t *testing.T) {
	w := newWorld(t)
	got := w.a.get("/nodes/" + w.origin)
	if got.status != http.StatusOK || got.field("status") != "ACTIVE" || got.field("nodeId") != w.origin {
		t.Fatalf("getNode = %d %s", got.status, got.raw)
	}
	if prof, ok := got.decoded.(map[string]any)["operationalProfile"].(map[string]any); !ok || prof["declaredAt"] == nil || prof["holidays"] == nil {
		t.Fatalf("getNode lacks the current profile: %s", got.raw)
	}
	list := w.a.get("/nodes?type=DELIVERY_STATION&region=region-1&status=ACTIVE")
	nodes, _ := list.decoded.(map[string]any)["nodes"].([]any)
	if list.status != http.StatusOK || len(nodes) != 1 {
		t.Fatalf("listNodes filtered = %d %s", list.status, list.raw)
	}
	if bad := w.a.get("/nodes?type=SPACE_STATION"); bad.status != http.StatusUnprocessableEntity || bad.slug() != "validation-failed" {
		t.Fatalf("listNodes with an unknown type = %d %s", bad.status, bad.raw)
	}
}

func TestNodeLifecycleRoutes(t *testing.T) {
	w := newWorld(t)
	sus := w.a.keyed(http.MethodPost, "/nodes/"+w.station+":suspend", `{"reason":"dock closed"}`)
	if sus.status != http.StatusOK || sus.field("status") != "SUSPENDED" || sus.field("suspendReason") != "dock closed" {
		t.Fatalf("suspendNode = %d %s", sus.status, sus.raw)
	}
	if r := w.a.keyed(http.MethodPost, "/nodes/"+w.station+":activate", ""); r.status != http.StatusOK || r.field("status") != "ACTIVE" {
		t.Fatalf("reactivate = %d %s", r.status, r.raw)
	}
	ret := w.a.keyed(http.MethodPost, "/nodes/"+w.station+":retire", "")
	if ret.status != http.StatusOK || ret.field("status") != "RETIRED" {
		t.Fatalf("retireNode = %d %s", ret.status, ret.raw)
	}
	if again := w.a.keyed(http.MethodPost, "/nodes/"+w.station+":activate", ""); again.status != http.StatusConflict || again.slug() != "node-retired" {
		t.Fatalf("activating a retired node = %d %s", again.status, again.raw)
	}
}

func TestEmptyListsAreArraysNotNull(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	for path, want := range map[string]string{"/nodes": `{"nodes":[]}`, "/lanes": `{"lanes":[]}`, "/regions": `[]`} {
		r := a.get(path)
		if r.status != http.StatusOK || trimNL(r.raw) != want {
			t.Errorf("GET %s = %d %q, want 200 %s", path, r.status, r.raw, want)
		}
	}
}

func TestLaneRoutesEndToEnd(t *testing.T) {
	w := newWorld(t)
	a := w.a
	open := a.keyed(http.MethodPost, "/lanes", `{"originNodeId":"`+w.origin+`","destinationNodeId":"`+w.dest+`","mode":"GROUND","nominalTransitMinutes":90}`)
	if open.status != http.StatusCreated || open.field("status") != "OPEN" || open.field("originNodeId") != w.origin {
		t.Fatalf("openLane = %d %s", open.status, open.raw)
	}
	id := open.field("laneId")

	list := a.get("/lanes?origin=" + w.origin + "&destination=" + w.dest)
	lanes, _ := list.decoded.(map[string]any)["lanes"].([]any)
	if list.status != http.StatusOK || len(lanes) != 1 {
		t.Fatalf("listLanes = %d %s", list.status, list.raw)
	}
	if other := a.get("/lanes?origin=nobody"); trimNL(other.raw) != `{"lanes":[]}` {
		t.Fatalf("listLanes of another origin = %s", other.raw)
	}

	closed := a.keyed(http.MethodPost, "/lanes/"+id+":close", "")
	if closed.status != http.StatusOK || closed.field("status") != "CLOSED" || closed.field("closedAt") == "" {
		t.Fatalf("closeLane = %d %s", closed.status, closed.raw)
	}
	if again := a.keyed(http.MethodPost, "/lanes/"+id+":close", ""); again.status != http.StatusConflict || again.slug() != "illegal-transition" {
		t.Fatalf("closing twice = %d %s", again.status, again.raw)
	}
}

func TestServiceAreaRoutesEndToEnd(t *testing.T) {
	w := newWorld(t)
	a := w.a
	put := a.keyed(http.MethodPut, "/service-areas/01310", `{"deliveryStationNodeId":"`+w.station+`","assignmentVersion":1}`)
	if put.status != http.StatusOK || put.field("postalPrefix") != "01310" || put.field("regionId") != "region-1" {
		t.Fatalf("assignServiceArea = %d %s", put.status, put.raw)
	}
	got := a.get("/service-areas/01310")
	if got.status != http.StatusOK || got.field("deliveryStationNodeId") != w.station {
		t.Fatalf("resolveServiceArea = %d %s", got.status, got.raw)
	}
	if stale := a.keyed(http.MethodPut, "/service-areas/01310", `{"deliveryStationNodeId":"`+w.station+`","assignmentVersion":1}`); stale.status != http.StatusConflict || stale.slug() != "stale-assignment-version" {
		t.Fatalf("stale reassignment = %d %s", stale.status, stale.raw)
	}
	if miss := a.get("/service-areas/99999"); miss.status != http.StatusNotFound || miss.slug() != "not-found" {
		t.Fatalf("unknown prefix = %d %s", miss.status, miss.raw)
	}
}

func TestRegionRoutesEndToEnd(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	def := a.keyed(http.MethodPost, "/regions", `{"code":"NE","name":"Northeast"}`)
	if def.status != http.StatusCreated || def.field("code") != "NE" || def.field("regionId") == "" {
		t.Fatalf("defineRegion = %d %s", def.status, def.raw)
	}
	if dup := a.keyed(http.MethodPost, "/regions", `{"code":"NE","name":"Other"}`); dup.status != http.StatusConflict || dup.slug() != "region-code-taken" {
		t.Fatalf("duplicate code = %d %s", dup.status, dup.raw)
	}
	a.keyed(http.MethodPost, "/regions", `{"code":"AA","name":"Alpha"}`)
	list := a.get("/regions")
	items, _ := list.decoded.([]any)
	if list.status != http.StatusOK || len(items) != 2 {
		t.Fatalf("listRegions = %d %s", list.status, list.raw)
	}
	one := a.get("/regions/" + def.field("regionId"))
	if one.status != http.StatusOK || one.field("name") != "Northeast" {
		t.Fatalf("getRegion = %d %s", one.status, one.raw)
	}
	if miss := a.get("/regions/region-nope"); miss.status != http.StatusNotFound || miss.slug() != "not-found" || !miss.isProblem() {
		t.Fatalf("unknown region = %d %s", miss.status, miss.raw)
	}
}

func TestDomainRefusalsMapToTheirProblems(t *testing.T) {
	w := newWorld(t)
	a := w.a
	cases := []struct {
		name         string
		method, path string
		body         string
		status       int
		slug         string
	}{
		{"unknown node", http.MethodGet, "/nodes/node-nope", "", 404, "not-found"},
		{"unknown region", http.MethodPost, "/nodes", `{"nodeType":"SORTATION_CENTER","operator":"NETWORK","regionId":"region-nope","timezone":"America/Sao_Paulo","location":{"postalCode":"1","countryCode":"BR"}}`, 422, "unknown-region"},
		{"operator not allowed", http.MethodPost, "/nodes", registerBody("DELIVERY_STATION", "VENDOR", `,"vendorRef":{"vendorId":"v","vendorNodeCode":"c"}`), 422, "operator-not-allowed-for-node-type"},
		{"subtype not allowed", http.MethodPost, "/nodes", registerBody("DELIVERY_STATION", "NETWORK", `,"subtype":"XL"`), 422, "subtype-not-allowed"},
		{"validation", http.MethodPost, "/nodes", registerBody("NOT_A_TYPE", "NETWORK", ""), 422, "validation-failed"},
		{"stale revision", http.MethodPut, "/nodes/" + w.origin + "/operational-profile", profileWith(""), 409, "stale-profile-revision"},
		{"sort dwell", http.MethodPut, "/nodes/" + w.origin + "/operational-profile", revision2(profileWith(`,"sortDwellMinutes":5`)), 422, "sort-dwell-not-allowed"},
		{"lane loop", http.MethodPost, "/lanes", `{"originNodeId":"` + w.origin + `","destinationNodeId":"` + w.origin + `","mode":"GROUND","nominalTransitMinutes":10}`, 422, "lane-loop"},
		{"lane endpoint unknown", http.MethodPost, "/lanes", `{"originNodeId":"node-nope","destinationNodeId":"` + w.dest + `","mode":"GROUND","nominalTransitMinutes":10}`, 404, "not-found"},
		{"not a delivery station", http.MethodPut, "/service-areas/01310", `{"deliveryStationNodeId":"` + w.origin + `","assignmentVersion":1}`, 409, "not-a-delivery-station"},
		{"unknown close", http.MethodPost, "/lanes/lane-nope:close", "", 404, "not-found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r reply
			if tc.method == http.MethodGet {
				r = a.get(tc.path)
			} else {
				r = a.keyed(tc.method, tc.path, tc.body)
			}
			if r.status != tc.status || r.slug() != tc.slug || !r.isProblem() {
				t.Fatalf("%s %s = %d %s (%s), want %d %s", tc.method, tc.path, r.status, r.slug(), r.raw, tc.status, tc.slug)
			}
			if r.field("title") == "" || r.field("instance") == "" {
				t.Fatalf("problem lacks title or instance: %s", r.raw)
			}
		})
	}
}

func TestOperationalProfileInboundLimits(t *testing.T) {
	w := newWorld(t)
	const path = "/operational-profile"
	good := `,"inbound":{"maxCartonWeightG":25000,"maxCartonDimsMm":{"l":600,"w":400,"h":300},"acceptsPallets":true,"requiresSscc":false}`
	r := w.a.keyed(http.MethodPut, "/nodes/"+w.origin+path, revision2(profileWith(good)))
	if r.status != http.StatusOK {
		t.Fatalf("declare with inbound: %d %s", r.status, r.raw)
	}
	in := r.decoded.(map[string]any)["operationalProfile"].(map[string]any)["inbound"].(map[string]any)
	if in["maxCartonWeightG"] != float64(25000) || in["acceptsPallets"] != true || in["requiresSscc"] != false ||
		in["maxCartonDimsMm"].(map[string]any)["h"] != float64(300) {
		t.Fatalf("inbound echoed as %v", in)
	}
	got := w.a.get("/nodes/" + w.origin)
	if got.decoded.(map[string]any)["operationalProfile"].(map[string]any)["inbound"] == nil {
		t.Fatalf("GET lacks the stored inbound limits: %s", got.raw)
	}
	// a later revision without inbound declares none: omitted, never null
	r = w.a.keyed(http.MethodPut, "/nodes/"+w.origin+path, strings.Replace(profileWith(""), `"profileRevision":1`, `"profileRevision":3`, 1))
	if r.status != http.StatusOK {
		t.Fatalf("declare without inbound: %d %s", r.status, r.raw)
	}
	if _, present := r.decoded.(map[string]any)["operationalProfile"].(map[string]any)["inbound"]; present {
		t.Fatalf("inbound must be omitted when none is declared: %s", r.raw)
	}
}

func TestOperationalProfileInboundRefusals(t *testing.T) {
	w := newWorld(t)
	tests := []struct {
		name, inbound string
		status        int
		slug          string
	}{
		{"zero weight", `{"maxCartonWeightG":0}`, 422, "validation-failed"},
		{"negative weight", `{"maxCartonWeightG":-5}`, 422, "validation-failed"},
		{"zero edge", `{"maxCartonDimsMm":{"l":600,"w":0,"h":300}}`, 422, "validation-failed"},
		{"missing edge", `{"maxCartonDimsMm":{"l":600,"w":400}}`, 422, "validation-failed"},
		{"empty dims", `{"maxCartonDimsMm":{}}`, 422, "validation-failed"},
		{"empty inbound", `{}`, 422, "validation-failed"},
		{"unknown inbound field", `{"maxCartonWeightKg":2}`, 400, "malformed-request-body"},
		{"fractional weight", `{"maxCartonWeightG":2.5}`, 400, "malformed-request-body"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := w.a.keyed(http.MethodPut, "/nodes/"+w.origin+"/operational-profile", revision2(profileWith(`,"inbound":`+tc.inbound)))
			if r.status != tc.status || r.slug() != tc.slug || !r.isProblem() {
				t.Fatalf("= %d %s (%s), want %d %s", r.status, r.slug(), r.raw, tc.status, tc.slug)
			}
		})
	}
}
