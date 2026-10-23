// Package memory holds in-memory TEST DOUBLES of the application's out ports.
// They are for unit, BDD and contract tests only: cmd/** never imports this
// package (enforced by internal/architecture) because the service runs on real
// Postgres and Kafka.
//
// Every repository round-trips its aggregate through Snapshot and Restore, so
// a loaded aggregate never aliases stored state, a corrupt row can not become a
// live aggregate and the doubles keep the persistence contract of the real
// adapters: a miss is ports.ErrNotFound, a stale or duplicate save is
// ports.ErrConcurrentUpdate, the unique indexes raise the domain's
// ErrVendorNodeCodeTaken, ErrDuplicateLane and ErrRegionCodeTaken.
//
// One Store backs every double. Its UnitOfWork is atomic: it holds the store
// for the whole unit and restores the state taken at the start when the unit
// fails or panics, outbox included.
package memory

import (
	"context"
	"sync"

	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// Store is the shared state of the in-memory doubles.
type Store struct {
	mu       sync.Mutex
	nodes    map[node.ID]node.Snapshot
	profiles map[node.ID][]node.OperationalProfile
	lanes    map[lane.ID]lane.Snapshot
	areas    map[servicearea.PostalPrefix]servicearea.Snapshot
	regions  map[region.ID]region.Snapshot
	outbox   []shared.Event
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		nodes:    map[node.ID]node.Snapshot{},
		profiles: map[node.ID][]node.OperationalProfile{},
		lanes:    map[lane.ID]lane.Snapshot{},
		areas:    map[servicearea.PostalPrefix]servicearea.Snapshot{},
		regions:  map[region.ID]region.Snapshot{},
	}
}

// state is a copy of everything a unit of work can change. Stored snapshots
// are replaced, never edited in place, so copying the maps is enough.
type state struct {
	nodes    map[node.ID]node.Snapshot
	profiles map[node.ID][]node.OperationalProfile
	lanes    map[lane.ID]lane.Snapshot
	areas    map[servicearea.PostalPrefix]servicearea.Snapshot
	regions  map[region.ID]region.Snapshot
	outbox   []shared.Event
}

func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) capture() state {
	return state{
		nodes: cloneMap(s.nodes), profiles: cloneMap(s.profiles), lanes: cloneMap(s.lanes),
		areas: cloneMap(s.areas), regions: cloneMap(s.regions),
		outbox: append([]shared.Event(nil), s.outbox...),
	}
}

func (s *Store) restore(st state) {
	s.nodes, s.profiles, s.lanes, s.areas, s.regions, s.outbox = st.nodes, st.profiles, st.lanes, st.areas, st.regions, st.outbox
}

// txKey marks a ctx that carries the open unit of work of one store.
type txKey struct{ store *Store }

// enter locks the store for one repository call, unless ctx already carries the
// unit of work that holds it. The returned func releases the lock.
func (s *Store) enter(ctx context.Context) func() {
	if ctx.Value(txKey{s}) != nil {
		return func() {}
	}
	s.mu.Lock()
	return s.mu.Unlock
}

// Nodes returns the NodeRepository double.
func (s *Store) Nodes() *NodeRepository { return &NodeRepository{s} }

// Lanes returns the LaneRepository double.
func (s *Store) Lanes() *LaneRepository { return &LaneRepository{s} }

// ServiceAreas returns the ServiceAreaRepository double.
func (s *Store) ServiceAreas() *ServiceAreaRepository { return &ServiceAreaRepository{s} }

// Regions returns the RegionRepository double.
func (s *Store) Regions() *RegionRepository { return &RegionRepository{s} }

// Outbox returns the EventOutbox double.
func (s *Store) Outbox() *Outbox { return &Outbox{s} }

// UnitOfWork returns the UnitOfWork double.
func (s *Store) UnitOfWork() *UnitOfWork { return &UnitOfWork{s} }

// Counts is how many rows each table of the store holds.
type Counts struct {
	Nodes, ProfileRevisions, Lanes, ServiceAreas, Regions, OutboxEvents int
	// VersionSum is the sum of every aggregate version: any saved change moves it.
	VersionSum int64
}

// Counts returns the size of every table and the sum of the aggregate
// versions, so a test can prove that a failed use case stored and changed
// nothing.
func (s *Store) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions := 0
	for _, rows := range s.profiles {
		revisions += len(rows)
	}
	var versions int64
	for _, n := range s.nodes {
		versions += n.Version
	}
	for _, l := range s.lanes {
		versions += l.Version
	}
	for _, a := range s.areas {
		versions += a.Version
	}
	for _, r := range s.regions {
		versions += r.Version
	}
	return Counts{
		VersionSum: versions, Nodes: len(s.nodes), ProfileRevisions: revisions, Lanes: len(s.lanes),
		ServiceAreas: len(s.areas), Regions: len(s.regions), OutboxEvents: len(s.outbox),
	}
}
