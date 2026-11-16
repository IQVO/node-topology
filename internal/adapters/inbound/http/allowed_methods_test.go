package http

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestAllowedMethodsListsEveryRegisteredMethod covers a path with several
// methods, so the list is derived from the router and not hard-coded to GET.
func TestAllowedMethodsListsEveryRegisteredMethod(t *testing.T) {
	r := chi.NewRouter()
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.Get("/x", noop)
	r.Post("/x", noop)
	r.Delete("/x", noop)
	r.Get("/y", noop)
	for path, want := range map[string]string{"/x": "GET, POST, DELETE", "/y": "GET", "/nope": ""} {
		if got := allowedMethods(r, path); got != want {
			t.Errorf("allowedMethods(%s) = %q, want %q", path, got, want)
		}
	}
}
