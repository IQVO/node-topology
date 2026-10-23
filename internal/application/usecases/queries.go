package usecases

import (
	"context"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// GetNode reads one node with its current profile (Open Host Service).
type GetNode struct{ nodes ports.NodeRepository }

// NewGetNode builds the query.
func NewGetNode(nodes ports.NodeRepository) *GetNode { return &GetNode{nodes} }

// Execute returns the node, or ports.ErrNotFound.
func (q *GetNode) Execute(ctx context.Context, id node.ID) (views.Node, error) {
	n, err := q.nodes.Get(ctx, id)
	if err != nil {
		return views.Node{}, err
	}
	return views.NodeFrom(n), nil
}

// ListNodesQuery filters ListNodes; an empty field does not filter and the
// set ones combine with AND.
type ListNodesQuery struct {
	Type     node.Type
	RegionID region.ID
	Status   node.Status
}

// ListNodes lists nodes by type, region and status.
type ListNodes struct{ nodes ports.NodeRepository }

// NewListNodes builds the query.
func NewListNodes(nodes ports.NodeRepository) *ListNodes { return &ListNodes{nodes} }

// Execute returns the matching nodes by id ascending, an empty list when none
// match. A type or status that is not an enumeration value is
// shared.ErrValidation.
func (q *ListNodes) Execute(ctx context.Context, query ListNodesQuery) ([]views.Node, error) {
	if query.Type != "" && !query.Type.Valid() {
		return nil, shared.Invalid("unknown node type %q", query.Type)
	}
	if query.Status != "" && !query.Status.Valid() {
		return nil, shared.Invalid("unknown node status %q", query.Status)
	}
	nodes, err := q.nodes.List(ctx, repository.NodeFilter{
		Type: query.Type, RegionID: query.RegionID, Status: query.Status,
	})
	if err != nil {
		return nil, err
	}
	out := make([]views.Node, len(nodes))
	for i, n := range nodes {
		out[i] = views.NodeFrom(n)
	}
	return out, nil
}

// GetRegion reads one region (Open Host Service).
type GetRegion struct{ regions ports.RegionRepository }

// NewGetRegion builds the query.
func NewGetRegion(regions ports.RegionRepository) *GetRegion { return &GetRegion{regions} }

// Execute returns the region, or ports.ErrNotFound.
func (q *GetRegion) Execute(ctx context.Context, id region.ID) (views.Region, error) {
	r, err := q.regions.Get(ctx, id)
	if err != nil {
		return views.Region{}, err
	}
	return views.RegionFrom(r), nil
}

// ListRegions lists every region (Open Host Service).
type ListRegions struct{ regions ports.RegionRepository }

// NewListRegions builds the query.
func NewListRegions(regions ports.RegionRepository) *ListRegions { return &ListRegions{regions} }

// Execute returns every region by id ascending, an empty list when none is
// defined. Regions are not paginated (single digits of regions).
func (q *ListRegions) Execute(ctx context.Context) ([]views.Region, error) {
	regions, err := q.regions.List(ctx)
	if err != nil {
		return nil, err
	}
	return views.RegionsFrom(regions), nil
}

// ListLanesQuery filters ListLanes; an empty field does not filter.
type ListLanesQuery struct {
	Origin      node.ID
	Destination node.ID
}

// ListLanes lists lanes by origin and destination.
type ListLanes struct{ lanes ports.LaneRepository }

// NewListLanes builds the query.
func NewListLanes(lanes ports.LaneRepository) *ListLanes { return &ListLanes{lanes} }

// Execute returns the matching lanes (open and closed) by id ascending, an
// empty list when none match.
func (q *ListLanes) Execute(ctx context.Context, query ListLanesQuery) ([]views.Lane, error) {
	lanes, err := q.lanes.List(ctx, repository.LaneFilter{Origin: query.Origin, Destination: query.Destination})
	if err != nil {
		return nil, err
	}
	out := make([]views.Lane, len(lanes))
	for i, l := range lanes {
		out[i] = views.LaneFrom(l)
	}
	return out, nil
}

// ResolveServiceArea resolves which delivery station serves a postal prefix.
type ResolveServiceArea struct{ areas ports.ServiceAreaRepository }

// NewResolveServiceArea builds the query.
func NewResolveServiceArea(areas ports.ServiceAreaRepository) *ResolveServiceArea {
	return &ResolveServiceArea{areas}
}

// Execute returns the current assignment of the prefix, matched exactly, or
// ports.ErrNotFound for an unassigned one.
func (q *ResolveServiceArea) Execute(ctx context.Context, prefix servicearea.PostalPrefix) (views.ServiceArea, error) {
	a, err := q.areas.Get(ctx, prefix)
	if err != nil {
		return views.ServiceArea{}, err
	}
	return views.ServiceAreaFrom(a), nil
}
