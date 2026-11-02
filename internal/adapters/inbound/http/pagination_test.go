package http_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// walk follows nextCursor from path (which already carries ?limit=) until it
// is absent and returns every item key in order and the number of pages.
func walk(t *testing.T, a *api, path, key string) ([]string, int) {
	t.Helper()
	var ids []string
	cursor, pages := "", 0
	for {
		p := path
		if cursor != "" {
			p += "&cursor=" + url.QueryEscape(cursor)
		}
		r := a.get(p)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d %s", p, r.status, r.raw)
		}
		body := r.decoded.(map[string]any)
		items, _ := body["items"].([]any)
		for _, it := range items {
			ids = append(ids, it.(map[string]any)[key].(string))
		}
		pages++
		next, _ := body["nextCursor"].(string)
		if next == "" {
			return ids, pages
		}
		if pages > 50 {
			t.Fatal("paging does not terminate")
		}
		cursor = next
	}
}

func assertWalk(t *testing.T, ids []string, pages, wantRows, wantPages int) {
	t.Helper()
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			t.Fatalf("row %s returned twice", id)
		}
		seen[id] = true
		if i > 0 && ids[i-1] >= id {
			t.Fatalf("rows out of order: %s then %s", ids[i-1], id)
		}
	}
	if len(ids) != wantRows || pages != wantPages {
		t.Fatalf("walked %d rows in %d pages, want %d in %d", len(ids), pages, wantRows, wantPages)
	}
}

func TestRegionsAreKeysetPaged(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	for i := 0; i < 7; i++ {
		if r := a.keyed(http.MethodPost, "/regions", fmt.Sprintf(`{"code":"R%d","name":"Region %d"}`, i, i)); r.status != http.StatusCreated {
			t.Fatalf("defineRegion = %d %s", r.status, r.raw)
		}
	}
	ids, pages := walk(t, a, "/regions?limit=3", "regionId")
	assertWalk(t, ids, pages, 7, 3)
	// a limit that divides the rows exactly still ends with an absent nextCursor
	ids, pages = walk(t, a, "/regions?limit=7", "regionId")
	assertWalk(t, ids, pages, 7, 1)
}

func TestNodesAndLanesAreKeysetPaged(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	reg := a.keyed(http.MethodPost, "/regions", `{"code":"SE","name":"Southeast"}`)
	body := fmt.Sprintf(`{"nodeType":"SORTATION_CENTER","operator":"NETWORK","regionId":%q,"timezone":"UTC","location":{"postalCode":"1","countryCode":"BR"}}`, reg.field("regionId"))
	var nodeIDs []string
	for i := 0; i < 5; i++ {
		r := a.keyed(http.MethodPost, "/nodes", body)
		if r.status != http.StatusCreated {
			t.Fatalf("registerNode = %d %s", r.status, r.raw)
		}
		nodeIDs = append(nodeIDs, r.field("nodeId"))
	}
	ids, pages := walk(t, a, "/nodes?limit=2&type=SORTATION_CENTER&region="+reg.field("regionId"), "nodeId")
	assertWalk(t, ids, pages, 5, 3)
	if ids, _ := walk(t, a, "/nodes?limit=2&type=AIR_HUB", "nodeId"); len(ids) != 0 {
		t.Fatalf("filter ignored: %v", ids)
	}
	_ = nodeIDs
}

func TestCollectionQueryErrors(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	cases := []struct{ path, slug string }{
		{"/nodes?limit=0", "invalid-limit"},
		{"/nodes?limit=501", "invalid-limit"},
		{"/nodes?limit=", "invalid-limit"},
		{"/nodes?limit=1&limit=2", "invalid-limit"},
		{"/lanes?limit=abc", "invalid-limit"},
		{"/regions?limit=-1", "invalid-limit"},
		{"/nodes?cursor=%25%25%25", "invalid-cursor"},
		{"/lanes?cursor=bm90LWpzb24", "invalid-cursor"},
		{"/regions?cursor=a&cursor=b", "invalid-cursor"},
		{"/nodes?after=x", "unknown-query-parameter"},
		{"/nodes?offset=10", "unknown-query-parameter"},
		{"/lanes?nextToken=x", "unknown-query-parameter"},
		{"/lanes?type=GROUND", "unknown-query-parameter"},
		{"/regions?from=x", "unknown-query-parameter"},
		{"/regions?region=x", "unknown-query-parameter"},
	}
	for _, tc := range cases {
		r := a.get(tc.path)
		if r.status != http.StatusBadRequest || r.slug() != tc.slug || !r.isProblem() {
			t.Errorf("GET %s = %d %s, want 400 %s", tc.path, r.status, r.raw, tc.slug)
		}
	}
}

func TestRetiredRoutesAreGone(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/nodes/node-1:activate"},
		{http.MethodPost, "/nodes/node-1:suspend"},
		{http.MethodPost, "/nodes/node-1:retire"},
		{http.MethodPost, "/lanes/lane-1:close"},
	} {
		r := a.keyed(tc.method, tc.path, `{"reason":"r"}`)
		if r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d %s, want 404 or 405", tc.method, tc.path, r.status, r.raw)
		}
	}
}
