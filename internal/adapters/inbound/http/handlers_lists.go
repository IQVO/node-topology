package http

import (
	"net/http"

	"github.com/claudioed/node-topology/internal/adapters/inbound/http/page"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// pageParams reads the keyset window of a collection GET: it rejects a query
// parameter the collection does not declare (unknown-query-parameter), an
// invalid limit and an undecodable cursor, writing the 400 problem itself.
// It returns the page size and the id the page starts after ("" = first page).
func pageParams(w http.ResponseWriter, r *http.Request, filters ...string) (limit int, afterID string, ok bool) {
	if name := page.Unknown(r, filters...); name != "" {
		writeProblem(w, r, unknownQueryParameter, "the query parameter "+sanitizeLog(name)+" is not declared by this collection")
		return 0, "", false
	}
	limit, err := page.Limit(r)
	if err != nil {
		writeProblem(w, r, invalidLimit, err.Error())
		return 0, "", false
	}
	pos, err := page.After(r)
	if err != nil {
		writeProblem(w, r, invalidCursor, err.Error())
		return 0, "", false
	}
	if pos != nil {
		afterID = pos.ID
	}
	return limit, afterID, true
}

// The keyset of every collection here is the unique id, so the cursor carries
// it as both the sort key and the tiebreaker.
func idPosition(id string) page.Position { return page.Position{Sort: id, ID: id} }

// listNodes is GET /nodes?type=&region=&status=&limit=&cursor=.
func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	limit, after, ok := pageParams(w, r, "type", "region", "status")
	if !ok {
		return
	}
	q := r.URL.Query()
	nodes, err := s.ListNodes.Execute(r.Context(), usecases.ListNodesQuery{
		Type: node.Type(q.Get("type")), RegionID: region.ID(q.Get("region")), Status: node.Status(q.Get("status")),
		AfterID: after, Limit: page.FetchSize(limit),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page.Of(nodes, limit, func(n views.Node) page.Position { return idPosition(n.NodeID) }))
}

// listLanes is GET /lanes?origin=&destination=&limit=&cursor=.
func (s *Server) listLanes(w http.ResponseWriter, r *http.Request) {
	limit, after, ok := pageParams(w, r, "origin", "destination")
	if !ok {
		return
	}
	q := r.URL.Query()
	lanes, err := s.ListLanes.Execute(r.Context(), usecases.ListLanesQuery{
		Origin: node.ID(q.Get("origin")), Destination: node.ID(q.Get("destination")),
		AfterID: after, Limit: page.FetchSize(limit),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page.Of(lanes, limit, func(l views.Lane) page.Position { return idPosition(l.LaneID) }))
}

// listRegions is GET /regions?limit=&cursor=.
func (s *Server) listRegions(w http.ResponseWriter, r *http.Request) {
	limit, after, ok := pageParams(w, r)
	if !ok {
		return
	}
	regions, err := s.ListRegions.Execute(r.Context(), usecases.ListRegionsQuery{AfterID: after, Limit: page.FetchSize(limit)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page.Of(regions, limit, func(g views.Region) page.Position { return idPosition(g.RegionID) }))
}
