package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

func (s *Server) mountLanes(r chi.Router) {
	s.keyed(r, http.MethodPost, "/lanes", s.openLane)
	r.Get("/lanes", s.listLanes)
	s.keyed(r, http.MethodPost, "/lanes/{laneId}:close", s.closeLane)
}

func (s *Server) mountServiceAreas(r chi.Router) {
	s.keyed(r, http.MethodPut, "/service-areas/{postalPrefix}", s.assignServiceArea)
	r.Get("/service-areas/{postalPrefix}", s.resolveServiceArea)
}

func (s *Server) mountRegions(r chi.Router) {
	s.keyed(r, http.MethodPost, "/regions", s.defineRegion)
	r.Get("/regions", s.listRegions)
	r.Get("/regions/{regionId}", s.getRegion)
}

// openLane is POST /lanes.
func (s *Server) openLane(w http.ResponseWriter, r *http.Request) {
	var body openLaneBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	l, err := s.OpenLane.Execute(r.Context(), body.command())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

// listLanes is GET /lanes?origin=&destination=.
func (s *Server) listLanes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lanes, err := s.ListLanes.Execute(r.Context(), usecases.ListLanesQuery{
		Origin: node.ID(q.Get("origin")), Destination: node.ID(q.Get("destination")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if lanes == nil {
		lanes = []views.Lane{}
	}
	writeJSON(w, http.StatusOK, laneList{Lanes: lanes})
}

// closeLane is POST /lanes/{laneId}:close.
func (s *Server) closeLane(w http.ResponseWriter, r *http.Request) {
	l, err := s.CloseLane.Execute(r.Context(), lane.ID(chi.URLParam(r, "laneId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// assignServiceArea is PUT /service-areas/{postalPrefix}.
func (s *Server) assignServiceArea(w http.ResponseWriter, r *http.Request) {
	var body assignServiceAreaBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	a, err := s.AssignServiceArea.Execute(r.Context(), body.command(chi.URLParam(r, "postalPrefix")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// resolveServiceArea is GET /service-areas/{postalPrefix}.
func (s *Server) resolveServiceArea(w http.ResponseWriter, r *http.Request) {
	a, err := s.ResolveServiceArea.Execute(r.Context(), servicearea.PostalPrefix(chi.URLParam(r, "postalPrefix")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// defineRegion is POST /regions.
func (s *Server) defineRegion(w http.ResponseWriter, r *http.Request) {
	var body defineRegionBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	reg, err := s.DefineRegion.Execute(r.Context(), body.command())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, reg)
}

// listRegions is GET /regions: a bare array ordered by regionId.
func (s *Server) listRegions(w http.ResponseWriter, r *http.Request) {
	regions, err := s.ListRegions.Execute(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	if regions == nil {
		regions = []views.Region{}
	}
	writeJSON(w, http.StatusOK, regions)
}

// getRegion is GET /regions/{regionId}.
func (s *Server) getRegion(w http.ResponseWriter, r *http.Request) {
	reg, err := s.GetRegion.Execute(r.Context(), region.ID(chi.URLParam(r, "regionId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}
