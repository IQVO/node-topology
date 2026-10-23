package features_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// bddNow is the service clock of every scenario.
var bddNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// world is the state of one scenario: the in-memory store behind the use
// cases, the aliases the scenario gave to the aggregates it created, and the
// outcome of the last command.
type world struct {
	store *memory.Store
	deps  usecases.Deps

	regions map[string]region.ID
	nodes   map[string]node.ID
	lanes   map[string]lane.ID

	lastErr       error
	outboxBefore  int
	countsBefore  memory.Counts
	commandWasRun bool
}

func newWorld() *world {
	w := &world{}
	w.reset()
	return w
}

// reset wires a fresh empty store, clock and id sequence.
func (w *world) reset() {
	s := memory.NewStore()
	w.store = s
	w.deps = usecases.Deps{
		Nodes: s.Nodes(), Lanes: s.Lanes(), ServiceAreas: s.ServiceAreas(), Regions: s.Regions(),
		Outbox: s.Outbox(), UoW: s.UnitOfWork(), Clock: memory.NewClock(bddNow), IDs: &memory.SequentialIDs{},
	}
	w.regions, w.nodes, w.lanes = map[string]region.ID{}, map[string]node.ID{}, map[string]lane.ID{}
	w.lastErr, w.outboxBefore, w.countsBefore, w.commandWasRun = nil, 0, memory.Counts{}, false
}

func (w *world) ctx() context.Context { return context.Background() }

// run executes one command and remembers its error and where the outbox stood,
// so the Then steps can ask what the command published. A failed command is
// not a failed step: the scenario asserts the outcome.
func (w *world) run(command func() error) {
	w.outboxBefore = len(w.store.Outbox().Events())
	w.countsBefore = w.store.Counts()
	w.lastErr = command()
	w.commandWasRun = true
}

// given executes a precondition: it must succeed, or the scenario is broken.
func (w *world) given(what string, command func() error) error {
	if err := command(); err != nil {
		return fmt.Errorf("precondition %q failed: %w", what, err)
	}
	return nil
}

// published returns the names of the events the last command appended.
func (w *world) published() []string {
	events := w.store.Outbox().Events()[w.outboxBefore:]
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.EventName()
	}
	return names
}

func (w *world) regionID(code string) (region.ID, error) {
	id, ok := w.regions[code]
	if !ok {
		return "", fmt.Errorf("the scenario never defined region %q", code)
	}
	return id, nil
}

func (w *world) nodeID(alias string) (node.ID, error) {
	id, ok := w.nodes[alias]
	if !ok {
		return "", fmt.Errorf("the scenario never created node %q", alias)
	}
	return id, nil
}

func (w *world) laneID(alias string) (lane.ID, error) {
	id, ok := w.lanes[alias]
	if !ok {
		return "", fmt.Errorf("the scenario never opened lane %q", alias)
	}
	return id, nil
}

var errNoCommand = errors.New("no command was run in this scenario")
