package memory

import (
	"context"
	"fmt"
	"slices"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// checkVersion applies the optimistic-concurrency rule of the ports package:
// a create (version 1) needs the id to be free, an update needs the stored
// version to be version-1.
func checkVersion(stored int64, exists bool, version int64) error {
	if version == 1 {
		if exists {
			return fmt.Errorf("%w: id already stored", ports.ErrConcurrentUpdate)
		}
		return nil
	}
	if !exists || stored != version-1 {
		return fmt.Errorf("%w: stored version is not %d", ports.ErrConcurrentUpdate, version-1)
	}
	return nil
}

func sortedValues[K ~string, V any](m map[K]V) []V {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]V, len(keys))
	for i, k := range keys {
		out[i] = m[k]
	}
	return out
}

// NodeRepository is the in-memory ports.NodeRepository.
type NodeRepository struct{ s *Store }

// Get returns the node with its current profile, or ports.ErrNotFound.
func (r *NodeRepository) Get(ctx context.Context, id node.ID) (*node.Node, error) {
	defer r.s.enter(ctx)()
	snap, ok := r.s.nodes[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return node.Restore(snap)
}

// FindByVendorRef returns the node of the vendor's code, or ports.ErrNotFound.
func (r *NodeRepository) FindByVendorRef(ctx context.Context, vendorID, vendorNodeCode string) (*node.Node, error) {
	defer r.s.enter(ctx)()
	for _, snap := range sortedValues(r.s.nodes) {
		if snap.Vendor != nil && snap.Vendor.VendorID == vendorID && snap.Vendor.VendorNodeCode == vendorNodeCode {
			return node.Restore(snap)
		}
	}
	return nil, ports.ErrNotFound
}

// List returns the nodes matching filter by id ascending.
func (r *NodeRepository) List(ctx context.Context, f repository.NodeFilter) ([]*node.Node, error) {
	defer r.s.enter(ctx)()
	out := []*node.Node{}
	for _, snap := range sortedValues(r.s.nodes) {
		if (f.Type != "" && snap.Type != f.Type) ||
			(f.RegionID != "" && snap.RegionID != f.RegionID) ||
			(f.Status != "" && snap.Status != f.Status) {
			continue
		}
		n, err := node.Restore(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// ProfileRevisions returns every stored profile revision, oldest first, or
// ports.ErrNotFound when the node does not exist.
func (r *NodeRepository) ProfileRevisions(ctx context.Context, id node.ID) ([]node.OperationalProfile, error) {
	defer r.s.enter(ctx)()
	if _, ok := r.s.nodes[id]; !ok {
		return nil, ports.ErrNotFound
	}
	rows := r.s.profiles[id]
	out := make([]node.OperationalProfile, len(rows))
	for i, p := range rows {
		c, err := node.NewOperationalProfile(p.ProfileDeclaration, p.DeclaredAt)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// Save stores the snapshot and, when its profile revision is higher than the
// highest stored one, appends that revision as its own row.
func (r *NodeRepository) Save(ctx context.Context, n *node.Node) error {
	defer r.s.enter(ctx)()
	snap := n.Snapshot()
	stored, exists := r.s.nodes[snap.ID]
	if err := checkVersion(stored.Version, exists, snap.Version); err != nil {
		return err
	}
	if snap.Vendor != nil {
		for id, other := range r.s.nodes {
			if id != snap.ID && other.Vendor != nil && *other.Vendor == *snap.Vendor {
				return node.ErrVendorNodeCodeTaken
			}
		}
	}
	r.s.nodes[snap.ID] = snap
	rows := r.s.profiles[snap.ID]
	if snap.Profile != nil && (len(rows) == 0 || snap.Profile.Revision > rows[len(rows)-1].Revision) {
		r.s.profiles[snap.ID] = append(rows[:len(rows):len(rows)], *snap.Profile)
	}
	return nil
}

// LaneRepository is the in-memory ports.LaneRepository.
type LaneRepository struct{ s *Store }

// Get returns the lane, or ports.ErrNotFound.
func (r *LaneRepository) Get(ctx context.Context, id lane.ID) (*lane.Lane, error) {
	defer r.s.enter(ctx)()
	snap, ok := r.s.lanes[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return lane.Restore(snap)
}

// FindOpen returns the OPEN lane of the triple, or ports.ErrNotFound.
func (r *LaneRepository) FindOpen(ctx context.Context, origin, destination node.ID, mode lane.Mode) (*lane.Lane, error) {
	defer r.s.enter(ctx)()
	for _, snap := range sortedValues(r.s.lanes) {
		if snap.Status == lane.StatusOpen && snap.OriginNodeID == origin &&
			snap.DestinationNodeID == destination && snap.Mode == mode {
			return lane.Restore(snap)
		}
	}
	return nil, ports.ErrNotFound
}

// List returns the lanes matching filter by id ascending.
func (r *LaneRepository) List(ctx context.Context, f repository.LaneFilter) ([]*lane.Lane, error) {
	defer r.s.enter(ctx)()
	out := []*lane.Lane{}
	for _, snap := range sortedValues(r.s.lanes) {
		if (f.Origin != "" && snap.OriginNodeID != f.Origin) ||
			(f.Destination != "" && snap.DestinationNodeID != f.Destination) {
			continue
		}
		l, err := lane.Restore(snap)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// Save stores the lane; a second OPEN lane for the same triple is
// lane.ErrDuplicateLane.
func (r *LaneRepository) Save(ctx context.Context, l *lane.Lane) error {
	defer r.s.enter(ctx)()
	snap := l.Snapshot()
	stored, exists := r.s.lanes[snap.ID]
	if err := checkVersion(stored.Version, exists, snap.Version); err != nil {
		return err
	}
	if snap.Status == lane.StatusOpen {
		for id, other := range r.s.lanes {
			if id != snap.ID && other.Status == lane.StatusOpen && other.OriginNodeID == snap.OriginNodeID &&
				other.DestinationNodeID == snap.DestinationNodeID && other.Mode == snap.Mode {
				return lane.ErrDuplicateLane
			}
		}
	}
	r.s.lanes[snap.ID] = snap
	return nil
}

// ServiceAreaRepository is the in-memory ports.ServiceAreaRepository.
type ServiceAreaRepository struct{ s *Store }

// Get returns the assignment of the prefix, or ports.ErrNotFound.
func (r *ServiceAreaRepository) Get(ctx context.Context, prefix servicearea.PostalPrefix) (*servicearea.ServiceArea, error) {
	defer r.s.enter(ctx)()
	snap, ok := r.s.areas[prefix]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return servicearea.Restore(snap)
}

// Save stores the service area.
func (r *ServiceAreaRepository) Save(ctx context.Context, a *servicearea.ServiceArea) error {
	defer r.s.enter(ctx)()
	snap := a.Snapshot()
	stored, exists := r.s.areas[snap.PostalPrefix]
	if err := checkVersion(stored.Version, exists, snap.Version); err != nil {
		return err
	}
	r.s.areas[snap.PostalPrefix] = snap
	return nil
}

// RegionRepository is the in-memory ports.RegionRepository.
type RegionRepository struct{ s *Store }

// Get returns the region, or ports.ErrNotFound.
func (r *RegionRepository) Get(ctx context.Context, id region.ID) (*region.Region, error) {
	defer r.s.enter(ctx)()
	snap, ok := r.s.regions[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return region.Restore(snap)
}

// FindByCode returns the region with that code, or ports.ErrNotFound.
func (r *RegionRepository) FindByCode(ctx context.Context, code string) (*region.Region, error) {
	defer r.s.enter(ctx)()
	for _, snap := range sortedValues(r.s.regions) {
		if snap.Code == code {
			return region.Restore(snap)
		}
	}
	return nil, ports.ErrNotFound
}

// Save stores the region; a second region with the same code is
// region.ErrRegionCodeTaken.
func (r *RegionRepository) Save(ctx context.Context, g *region.Region) error {
	defer r.s.enter(ctx)()
	snap := g.Snapshot()
	stored, exists := r.s.regions[snap.ID]
	if err := checkVersion(stored.Version, exists, snap.Version); err != nil {
		return err
	}
	for id, other := range r.s.regions {
		if id != snap.ID && other.Code == snap.Code {
			return region.ErrRegionCodeTaken
		}
	}
	r.s.regions[snap.ID] = snap
	return nil
}
