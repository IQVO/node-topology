package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// mimeTypeJSON is the MIME type of every resource this server serves.
const mimeTypeJSON = "application/json"

// serviceAreaURITemplate is the ONE resource this server offers (Agentic
// Standard v2, AS-3): a scoped read model this context owns, not a dump of a
// table. It answers "which delivery station serves this postal prefix, in
// which region, at which assignment version?" for ONE prefix, built from the
// same ResolveServiceArea read use case the resolve_service_area tool and GET
// /service-areas/{postalPrefix} use. Bounded by construction: one assignment,
// four fields, JSON text.
const serviceAreaURITemplate = "warehouse://node-topology/service-areas/{postalPrefix}"

// registerResources adds the resource surface (read-only by nature, L1; the
// charter keeps resources context data an agent can load without a tool
// call). No resource ever writes: Deps holds read use cases only (ADR 0008).
func (d Deps) registerResources(server *mcp.Server) {
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: serviceAreaURITemplate,
		Name:        "service area of a postal prefix",
		Description: "The delivery-station assignment of one postal prefix (matched exactly): deliveryStationNodeId, regionId and assignmentVersion. An unassigned prefix is an error, an empty registry is an error for every prefix.",
		MIMEType:    mimeTypeJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		prefix, err := parseServiceAreaURI(req.Params.URI)
		if err != nil {
			return nil, err
		}
		area, err := d.ResolveServiceArea.Execute(ctx, servicearea.PostalPrefix(prefix))
		if err != nil {
			return nil, err
		}
		return resourceResult(req.Params.URI, toServiceArea(area))
	})
}

// resourceResult marshals one read model into the resource's text contents.
func resourceResult(uri string, view any) (*mcp.ReadResourceResult, error) {
	body, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: mimeTypeJSON,
			Text:     string(body),
		}},
	}, nil
}

// parseServiceAreaURI extracts the postalPrefix from a
// warehouse://node-topology/service-areas/{postalPrefix} URI. The prefix is
// model-supplied and therefore untrusted: a malformed URI or an empty
// prefix is rejected rather than silently defaulted.
func parseServiceAreaURI(uri string) (string, error) {
	const scheme = "warehouse://node-topology/service-areas/"
	rest, ok := strings.CutPrefix(uri, scheme)
	if !ok {
		return "", fmt.Errorf("resource uri %q must start with %q", uri, scheme)
	}
	if rest == "" || strings.Contains(rest, "/") {
		return "", fmt.Errorf("resource uri %q must carry exactly one non-empty postalPrefix after %q", uri, scheme)
	}
	return rest, nil
}
