package usecases_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/repository"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// env is one use-case test bench: the memory doubles behind fault-injecting
// wrappers, a fixed clock and sequential ids.
type env struct {
	t     *testing.T
	store *memory.Store
	clock *memory.Clock

	nodes   *flakyNodes
	lanes   *flakyLanes
	areas   *flakyAreas
	regions *flakyRegions
	outbox  *flakyOutbox
	deps    usecases.Deps
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s := memory.NewStore()
	e := &env{
		t: t, store: s, clock: memory.NewClock(repocontract.Now),
		nodes: &flakyNodes{NodeRepository: s.Nodes()}, lanes: &flakyLanes{LaneRepository: s.Lanes()},
		areas: &flakyAreas{ServiceAreaRepository: s.ServiceAreas()}, regions: &flakyRegions{RegionRepository: s.Regions()},
		outbox: &flakyOutbox{EventOutbox: s.Outbox()},
	}
	e.deps = usecases.Deps{
		Nodes: e.nodes, Lanes: e.lanes, ServiceAreas: e.areas, Regions: e.regions, Outbox: e.outbox,
		UoW: s.UnitOfWork(), Clock: e.clock, IDs: &memory.SequentialIDs{},
	}
	return e
}

// ---- fault injection ------------------------------------------------------

// failAt returns errs[i] for the i-th call, nil past the end.
func failAt(calls *int, errs []error) error {
	i := *calls
	*calls++
	if i < len(errs) {
		return errs[i]
	}
	return nil
}

type flakyNodes struct {
	ports.NodeRepository
	getErr, findErr, listErr error
	saveErrs                 []error
	saves                    int
}

func (f *flakyNodes) Get(ctx context.Context, id node.ID) (*node.Node, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.NodeRepository.Get(ctx, id)
}

func (f *flakyNodes) FindByVendorRef(ctx context.Context, v, c string) (*node.Node, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.NodeRepository.FindByVendorRef(ctx, v, c)
}

func (f *flakyNodes) List(ctx context.Context, fl repository.NodeFilter) ([]*node.Node, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.NodeRepository.List(ctx, fl)
}

func (f *flakyNodes) Save(ctx context.Context, n *node.Node) error {
	if err := failAt(&f.saves, f.saveErrs); err != nil {
		return err
	}
	return f.NodeRepository.Save(ctx, n)
}

type flakyLanes struct {
	ports.LaneRepository
	getErr, findErr, listErr error
	saveErrs                 []error
	saves                    int
}

func (f *flakyLanes) Get(ctx context.Context, id lane.ID) (*lane.Lane, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.LaneRepository.Get(ctx, id)
}

func (f *flakyLanes) FindOpen(ctx context.Context, o, d node.ID, m lane.Mode) (*lane.Lane, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.LaneRepository.FindOpen(ctx, o, d, m)
}

func (f *flakyLanes) List(ctx context.Context, fl repository.LaneFilter) ([]*lane.Lane, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.LaneRepository.List(ctx, fl)
}

func (f *flakyLanes) Save(ctx context.Context, l *lane.Lane) error {
	if err := failAt(&f.saves, f.saveErrs); err != nil {
		return err
	}
	return f.LaneRepository.Save(ctx, l)
}

type flakyAreas struct {
	ports.ServiceAreaRepository
	getErr   error
	saveErrs []error
	saves    int
}

func (f *flakyAreas) Get(ctx context.Context, p servicearea.PostalPrefix) (*servicearea.ServiceArea, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.ServiceAreaRepository.Get(ctx, p)
}

func (f *flakyAreas) Save(ctx context.Context, a *servicearea.ServiceArea) error {
	if err := failAt(&f.saves, f.saveErrs); err != nil {
		return err
	}
	return f.ServiceAreaRepository.Save(ctx, a)
}

type flakyRegions struct {
	ports.RegionRepository
	getErr, findErr, listErr error
	saveErrs                 []error
	saves                    int
}

func (f *flakyRegions) List(ctx context.Context) ([]*region.Region, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.RegionRepository.List(ctx)
}

func (f *flakyRegions) Get(ctx context.Context, id region.ID) (*region.Region, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.RegionRepository.Get(ctx, id)
}

func (f *flakyRegions) FindByCode(ctx context.Context, c string) (*region.Region, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.RegionRepository.FindByCode(ctx, c)
}

func (f *flakyRegions) Save(ctx context.Context, r *region.Region) error {
	if err := failAt(&f.saves, f.saveErrs); err != nil {
		return err
	}
	return f.RegionRepository.Save(ctx, r)
}

type flakyOutbox struct {
	ports.EventOutbox
	errs    []error
	appends int
}

func (f *flakyOutbox) Append(ctx context.Context, events ...shared.Event) error {
	if err := failAt(&f.appends, f.errs); err != nil {
		return err
	}
	return f.EventOutbox.Append(ctx, events...)
}

// ---- seeding ----------------------------------------------------------------

// seedRegion stores a region (no event).
func (e *env) seedRegion(id region.ID, code string) *region.Region {
	e.t.Helper()
	r := repocontract.NewRegion(e.t, id, code)
	if err := e.store.Regions().Save(context.Background(), r); err != nil {
		e.t.Fatalf("seed region: %v", err)
	}
	return r
}

// seedNode stores a node of the type in the status, at version 1 (no event). A
// vendor-run type is registered as vendor-1/code.
func (e *env) seedNode(reg *region.Region, typ node.Type, id node.ID, code string, status node.Status) {
	e.t.Helper()
	n := repocontract.NewNode(e.t, reg, typ, id, code)
	e.advance(n, status)
	snap := n.Snapshot()
	snap.Version = 1
	stored, err := node.Restore(snap)
	if err != nil {
		e.t.Fatalf("restore seeded node: %v", err)
	}
	if err := e.store.Nodes().Save(context.Background(), stored); err != nil {
		e.t.Fatalf("seed node: %v", err)
	}
}

// advance walks a PROPOSED node to the status through the domain commands.
func (e *env) advance(n *node.Node, status node.Status) {
	e.t.Helper()
	steps := map[node.Status][]func() error{
		node.StatusProposed:  {},
		node.StatusActive:    {e.declare(n), e.activate(n)},
		node.StatusSuspended: {e.declare(n), e.activate(n), e.suspend(n)},
		node.StatusRetired:   {e.declare(n), e.activate(n), e.retire(n)},
	}
	for _, step := range steps[status] {
		if err := step(); err != nil {
			e.t.Fatalf("advance to %s: %v", status, err)
		}
	}
}

func (e *env) declare(n *node.Node) func() error {
	return func() error { _, err := n.DeclareProfile(repocontract.Declaration(1), repocontract.Now); return err }
}

func (e *env) activate(n *node.Node) func() error {
	return func() error { _, err := n.Activate(repocontract.Now); return err }
}

func (e *env) suspend(n *node.Node) func() error {
	return func() error { _, err := n.Suspend("audit", repocontract.Now); return err }
}

func (e *env) retire(n *node.Node) func() error {
	return func() error { _, err := n.Retire(repocontract.Now); return err }
}

// seedLane stores an OPEN lane between two stored ACTIVE nodes.
func (e *env) seedLane(id lane.ID, origin, destination node.ID, mode lane.Mode) {
	e.t.Helper()
	ctx := context.Background()
	o, err := e.store.Nodes().Get(ctx, origin)
	if err != nil {
		e.t.Fatalf("seed lane origin: %v", err)
	}
	d, err := e.store.Nodes().Get(ctx, destination)
	if err != nil {
		e.t.Fatalf("seed lane destination: %v", err)
	}
	if err := e.store.Lanes().Save(ctx, repocontract.NewLane(e.t, id, o, d, mode)); err != nil {
		e.t.Fatalf("seed lane: %v", err)
	}
}

// seedArea stores an assignment of the prefix to a stored ACTIVE station.
func (e *env) seedArea(prefix servicearea.PostalPrefix, station node.ID, reg *region.Region, version int) {
	e.t.Helper()
	ctx := context.Background()
	st, err := e.store.Nodes().Get(ctx, station)
	if err != nil {
		e.t.Fatalf("seed area station: %v", err)
	}
	if err := e.store.ServiceAreas().Save(ctx, repocontract.NewServiceArea(e.t, prefix, st, reg, version)); err != nil {
		e.t.Fatalf("seed area: %v", err)
	}
}

// laneTypes returns the node types of an allowed lane pair.
func laneTypes(t *testing.T) (origin, destination node.Type) {
	t.Helper()
	return repocontract.AllowedPair(t)
}

// seedLaneEnds stores an ACTIVE origin ("n-o") and destination ("n-d") of an
// allowed pair and returns the region.
func (e *env) seedLaneEnds() *region.Region {
	e.t.Helper()
	reg := e.seedRegion("r-1", "SP")
	ot, dt := laneTypes(e.t)
	e.seedNode(reg, ot, "n-o", "O", node.StatusActive)
	e.seedNode(reg, dt, "n-d", "D", node.StatusActive)
	return reg
}

// ---- assertions -------------------------------------------------------------

// before captures the store's table sizes.
func (e *env) before() memory.Counts { return e.store.Counts() }

// assertUnchanged fails when any table has a different size than at before.
func (e *env) assertUnchanged(before memory.Counts) {
	e.t.Helper()
	if after := e.store.Counts(); after != before {
		e.t.Fatalf("store changed on a failed command: before %+v, after %+v", before, after)
	}
}

// assertRaised fails unless exactly the named events were appended since
// before, in order.
func (e *env) assertRaised(before memory.Counts, names ...string) []shared.Event {
	e.t.Helper()
	all := e.store.Outbox().Events()
	got := all[before.OutboxEvents:]
	gotNames := make([]string, len(got))
	for i, ev := range got {
		gotNames[i] = ev.EventName()
	}
	if len(names) == 0 {
		names = []string{}
	}
	if !reflect.DeepEqual(gotNames, names) {
		e.t.Fatalf("raised events = %v, want %v", gotNames, names)
	}
	return got
}

func wantErr(t testing.TB, got, want error) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("unexpected error: %v", got)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

var errBoom = errors.New("boom")

// mustRegion returns the stored region r-1.
func mustRegion(e *env) *region.Region {
	e.t.Helper()
	r, err := e.store.Regions().Get(context.Background(), "r-1")
	if err != nil {
		e.t.Fatalf("region r-1: %v", err)
	}
	return r
}
