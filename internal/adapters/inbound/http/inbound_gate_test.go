package http_test

import (
	"net/http"
	"testing"
)

// inboundBody is a profile revision 2 declaring inbound limits.
var inboundBody = revision2(profileWith(`,"inbound":{"maxCartonWeightG":25000,"acceptsPallets":true}`))

// capabilitiesOf returns the capabilities object of a node reply.
func capabilitiesOf(t *testing.T, r reply) map[string]any {
	t.Helper()
	caps, ok := r.decoded.(map[string]any)["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities in %s", r.raw)
	}
	return caps
}

// TestInboundLimitsOnANodeWithoutInboundAre422 checks the capability gate of
// ADR 0015: a delivery station may not declare inbound limits.
func TestInboundLimitsOnANodeWithoutInboundAre422(t *testing.T) {
	w := newWorld(t)
	r := w.a.keyed(http.MethodPut, "/nodes/"+w.station+"/operational-profile", inboundBody)
	if r.status != http.StatusUnprocessableEntity || r.slug() != "inbound-limits-not-allowed" || !r.isProblem() {
		t.Fatalf("= %d %s (%s), want 422 inbound-limits-not-allowed", r.status, r.slug(), r.raw)
	}
	if r.field("title") == "" || r.field("instance") == "" {
		t.Fatalf("problem lacks title or instance: %s", r.raw)
	}
	got := w.a.get("/nodes/" + w.station)
	profile, _ := got.decoded.(map[string]any)["operationalProfile"].(map[string]any)
	if profile["profileRevision"] != float64(1) || profile["inbound"] != nil {
		t.Fatalf("a refused declaration stored something: %s", got.raw)
	}
	// the same body without inbound is accepted
	if ok := w.a.keyed(http.MethodPut, "/nodes/"+w.station+"/operational-profile", revision2(profileWith(""))); ok.status != http.StatusOK {
		t.Fatalf("declare without inbound: %d %s", ok.status, ok.raw)
	}
}

// TestInboundGateBeatsTheValueChecks: an invalid inbound on a node without
// INBOUND is still inbound-limits-not-allowed, not validation-failed.
func TestInboundGateBeatsTheValueChecks(t *testing.T) {
	w := newWorld(t)
	body := revision2(profileWith(`,"inbound":{}`))
	if r := w.a.keyed(http.MethodPut, "/nodes/"+w.station+"/operational-profile", body); r.status != 422 || r.slug() != "inbound-limits-not-allowed" {
		t.Fatalf("empty inbound on a delivery station = %d %s, want 422 inbound-limits-not-allowed", r.status, r.slug())
	}
	if r := w.a.keyed(http.MethodPut, "/nodes/"+w.origin+"/operational-profile", body); r.status != 422 || r.slug() != "validation-failed" {
		t.Fatalf("empty inbound on a fulfillment centre = %d %s, want 422 validation-failed", r.status, r.slug())
	}
}

// TestFulfillmentCentreReportsInbound: GET /nodes/{nodeId} and GET /nodes
// report inbound:true for a FULFILLMENT_CENTER (ADR 0015).
func TestFulfillmentCentreReportsInbound(t *testing.T) {
	w := newWorld(t)
	fc := capabilitiesOf(t, w.a.get("/nodes/"+w.origin))
	if fc["inbound"] != true || fc["outbound"] != true || fc["lastMile"] != false {
		t.Fatalf("fulfillment centre capabilities = %v", fc)
	}
	if ds := capabilitiesOf(t, w.a.get("/nodes/"+w.station)); ds["inbound"] != false {
		t.Fatalf("delivery station capabilities = %v", ds)
	}
	list := w.a.get("/nodes?type=FULFILLMENT_CENTER")
	items, _ := list.decoded.(map[string]any)["items"].([]any)
	if list.status != http.StatusOK || len(items) != 1 {
		t.Fatalf("list fulfillment centres = %d %s", list.status, list.raw)
	}
	if caps, _ := items[0].(map[string]any)["capabilities"].(map[string]any); caps["inbound"] != true {
		t.Fatalf("listed fulfillment centre capabilities = %v", caps)
	}
}
