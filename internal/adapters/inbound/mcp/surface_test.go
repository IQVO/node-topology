// The MCP surface beyond tools (Agentic Standard v2, AS-3): ONE resource —
// the scoped service-area read model — and ONE prompt — this context's
// operator SOP. Pinned through the same in-memory client the tool tests use.
package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The resource template is advertised with the fleet URI shape and reads the
// same read model as the resolve_service_area tool.
func TestSurface_ResourceTemplateListedAndRead(t *testing.T) {
	h, w := withWorld(t)
	ctx := t.Context()

	listed, err := h.session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("list resource templates: %v", err)
	}
	found := false
	for _, rt := range listed.ResourceTemplates {
		if rt.URITemplate == "warehouse://node-topology/service-areas/{postalPrefix}" {
			found = true
			if rt.MIMEType != "application/json" {
				t.Errorf("template MIMEType = %q, want application/json", rt.MIMEType)
			}
		}
	}
	if !found {
		t.Fatalf("service-area resource template not advertised: %+v", listed.ResourceTemplates)
	}

	res, err := h.session.ReadResource(ctx, &sdk.ReadResourceParams{
		URI: "warehouse://node-topology/service-areas/013",
	})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents = %d entries, want 1", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != "warehouse://node-topology/service-areas/013" {
		t.Errorf("content URI = %q", c.URI)
	}
	var area struct {
		PostalPrefix          string `json:"postalPrefix"`
		DeliveryStationNodeID string `json:"deliveryStationNodeId"`
		RegionID              string `json:"regionId"`
		AssignmentVersion     int    `json:"assignmentVersion"`
	}
	if err := json.Unmarshal([]byte(c.Text), &area); err != nil {
		t.Fatalf("resource text is not the service-area JSON: %v (%s)", err, c.Text)
	}
	if area.PostalPrefix != "013" || area.DeliveryStationNodeID != w.dsNodeID || area.RegionID != w.regionID || area.AssignmentVersion != 1 {
		t.Errorf("service area = %+v, want prefix 013, station %s, region %s, version 1", area, w.dsNodeID, w.regionID)
	}
}

// A malformed URI and an unassigned prefix are errors, never silent defaults.
func TestSurface_ResourceRejectsMalformedAndUnknown(t *testing.T) {
	h, _ := withWorld(t)
	for _, uri := range []string{
		"staffing://node-topology/service-areas/013",        // wrong scheme
		"warehouse://node-topology/nodes/013",               // wrong model
		"warehouse://node-topology/service-areas/",          // empty prefix
		"warehouse://node-topology/service-areas/013/extra", // trailing segment
		"warehouse://node-topology/service-areas/999",       // unassigned prefix
	} {
		if _, err := h.session.ReadResource(t.Context(), &sdk.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("read resource %q: expected an error", uri)
		}
	}
}

// The prompt is advertised with its typed argument and returns the SOP text.
func TestSurface_PromptListedAndGet(t *testing.T) {
	h, _ := withWorld(t)
	ctx := t.Context()

	listed, err := h.session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(listed.Prompts) != 1 || listed.Prompts[0].Name != "resolve_delivery_coverage" {
		t.Fatalf("prompts = %+v, want exactly resolve_delivery_coverage", listed.Prompts)
	}
	if len(listed.Prompts[0].Arguments) != 1 || listed.Prompts[0].Arguments[0].Name != "postalPrefix" || !listed.Prompts[0].Arguments[0].Required {
		t.Fatalf("prompt arguments = %+v, want one required postalPrefix", listed.Prompts[0].Arguments)
	}

	res, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{
		Name:      "resolve_delivery_coverage",
		Arguments: map[string]string{"postalPrefix": "013"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(res.Messages))
	}
	text := res.Messages[0].Content.(*sdk.TextContent).Text
	for _, needle := range []string{"resolve_service_area", "get_node", "list_lanes", "013"} {
		if !strings.Contains(text, needle) {
			t.Errorf("SOP text does not mention %q", needle)
		}
	}

	// A missing argument degrades to the placeholder, never to a hidden call.
	noArg, err := h.session.GetPrompt(ctx, &sdk.GetPromptParams{Name: "resolve_delivery_coverage"})
	if err != nil {
		t.Fatalf("get prompt without arguments: %v", err)
	}
	if plain := noArg.Messages[0].Content.(*sdk.TextContent).Text; !strings.Contains(plain, "<postalPrefix>") {
		t.Errorf("SOP without an argument should carry the <postalPrefix> placeholder")
	}
}
