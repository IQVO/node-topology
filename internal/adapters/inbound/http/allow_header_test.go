package http_test

import (
	"net/http"
	"testing"
)

// TestWrongMethodCarriesAllowHeader pins RFC 9110 section 15.5.6: a 405 MUST
// list the methods the resource does support. chi sets Allow only in its
// DEFAULT 405 handler; once a custom handler is registered (the RFC 7807 one)
// it is called as-is, so the handler derives the list from the router itself.
func TestWrongMethodCarriesAllowHeader(t *testing.T) {
	cases := []struct{ method, path, wantAllow string }{
		{http.MethodDelete, "/healthz", "GET"},
		{http.MethodPost, "/readyz", "GET"},
		{http.MethodDelete, "/nodes", "GET, POST"},
		{http.MethodDelete, "/nodes/node-1", "GET"},
		{http.MethodDelete, "/nodes/a%2Fb", "GET"}, // chi routes on RawPath: the escaped slash stays in one segment
		{http.MethodGet, "/nodes/node-1/operational-profile", "PUT"},
		{http.MethodGet, "/nodes/node-1/activations", "POST"},
		{http.MethodPut, "/lanes", "GET, POST"},
		{http.MethodGet, "/lanes/lane-1/closures", "POST"},
		{http.MethodPost, "/service-areas/01310", "GET, PUT"},
		{http.MethodDelete, "/regions", "GET, POST"},
		{http.MethodPost, "/regions/region-1", "GET"},
	}
	a := newAPI(t)
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := a.do(tc.method, tc.path, "", "k")
			if r.status != http.StatusMethodNotAllowed || r.slug() != "method-not-allowed" || !r.isProblem() {
				t.Fatalf("status = %d %s, want a 405 method-not-allowed problem", r.status, r.raw)
			}
			if got := r.header.Get("Allow"); got != tc.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tc.wantAllow)
			}
		})
	}
}

// TestUnknownPathHasNoAllowHeader: a 404 is not a 405, so it lists no methods.
func TestUnknownPathHasNoAllowHeader(t *testing.T) {
	r := newAPI(t).do(http.MethodDelete, "/nowhere", "", "k")
	if r.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.status)
	}
	if got := r.header.Get("Allow"); got != "" {
		t.Errorf("Allow = %q on an unknown path, want empty", got)
	}
}
