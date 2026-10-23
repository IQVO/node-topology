package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

func (s *Server) mountNodes(r chi.Router) {
	s.keyed(r, http.MethodPost, "/nodes", s.registerNode)
	r.Get("/nodes", s.listNodes)
	r.Get("/nodes/{nodeId}", s.getNode)
	s.keyed(r, http.MethodPut, "/nodes/{nodeId}/operational-profile", s.declareOperationalProfile)
	s.keyed(r, http.MethodPost, "/nodes/{nodeId}:activate", s.nodeTransition(s.ActivateNode))
	s.keyed(r, http.MethodPost, "/nodes/{nodeId}:suspend", s.suspendNode)
	s.keyed(r, http.MethodPost, "/nodes/{nodeId}:retire", s.nodeTransition(s.RetireNode))
}

func nodeID(r *http.Request) node.ID { return node.ID(chi.URLParam(r, "nodeId")) }

// registerNode is POST /nodes.
func (s *Server) registerNode(w http.ResponseWriter, r *http.Request) {
	var body registerNodeBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.RegisterNode.Execute(r.Context(), body.command())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

// listNodes is GET /nodes?type=&region=&status=.
func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodes, err := s.ListNodes.Execute(r.Context(), usecases.ListNodesQuery{
		Type: node.Type(q.Get("type")), RegionID: region.ID(q.Get("region")), Status: node.Status(q.Get("status")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nodeList{Nodes: nonNilNodes(nodes)})
}

func nonNilNodes(in []views.Node) []views.Node {
	if in == nil {
		return []views.Node{}
	}
	return in
}

// getNode is GET /nodes/{nodeId}.
func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.GetNode.Execute(r.Context(), nodeID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// declareOperationalProfile is PUT /nodes/{nodeId}/operational-profile.
func (s *Server) declareOperationalProfile(w http.ResponseWriter, r *http.Request) {
	var body profileBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.DeclareProfile.Execute(r.Context(), usecases.DeclareOperationalProfileCommand{NodeID: nodeID(r), Declaration: body.declaration()})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// nodeTransition is POST /nodes/{nodeId}:activate and :retire, which take no
// body.
func (s *Server) nodeTransition(uc nodeTransition) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := uc.Execute(r.Context(), nodeID(r))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, n)
	}
}

// suspendNode is POST /nodes/{nodeId}:suspend.
func (s *Server) suspendNode(w http.ResponseWriter, r *http.Request) {
	var body suspendBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.SuspendNode.Execute(r.Context(), nodeID(r), body.Reason)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}
