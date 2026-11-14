package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

func (s *Server) mountLanes(r chi.Router) {
	s.keyed(r, http.MethodPost, "/lanes", s.openLane)
	r.Get("/lanes", s.listLanes)
	s.keyed(r, http.MethodPost, "/lanes/{laneId}/closures", s.closeLane)
	s.keyed(r, http.MethodPost, "/lanes/{laneId}/cost-declarations", s.declareLaneCost)
	s.keyed(r, http.MethodPost, "/lanes/{laneId}/emission-declarations", s.declareLaneEmission)
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

// closeLane is POST /lanes/{laneId}/closures.
func (s *Server) closeLane(w http.ResponseWriter, r *http.Request) {
	l, err := s.CloseLane.Execute(r.Context(), lane.ID(chi.URLParam(r, "laneId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// declareLaneCost is POST /lanes/{laneId}/cost-declarations.
func (s *Server) declareLaneCost(w http.ResponseWriter, r *http.Request) {
	var body declareLaneCostBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	l, err := s.DeclareLaneCost.Execute(r.Context(), body.command(chi.URLParam(r, "laneId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

// declareLaneEmission is POST /lanes/{laneId}/emission-declarations.
func (s *Server) declareLaneEmission(w http.ResponseWriter, r *http.Request) {
	var body declareLaneEmissionBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	l, err := s.DeclareLaneEmission.Execute(r.Context(), body.command(chi.URLParam(r, "laneId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
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

// getRegion is GET /regions/{regionId}.
func (s *Server) getRegion(w http.ResponseWriter, r *http.Request) {
	reg, err := s.GetRegion.Execute(r.Context(), region.ID(chi.URLParam(r, "regionId")))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}
