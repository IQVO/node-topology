//go:build integration

package http_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// A refusal that depends on state another request can change (ADR 0020) is
// RETRYABLE: nothing is stored under the key, the key is released, and the same
// request under the same key runs again. These tests run the production router,
// the real use cases and the real middleware over Postgres.

// keyRows counts the rows idempotency_keys holds for key.
func (d dbAPI) keyRows(key string) int {
	d.t.Helper()
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_keys WHERE key = $1`, key).Scan(&n); err != nil {
		d.t.Fatalf("count key %s: %v", key, err)
	}
	return n
}

func wasReplayed(r reply) bool { return r.header.Get(inboundhttp.IdempotencyReplayedHeader) == "true" }

// losesTheLock wraps the node repository: its next failSaves Save calls lose the
// optimistic lock, as when another request moved the same node first.
type losesTheLock struct {
	ports.NodeRepository
	failSaves atomic.Int32
}

func (l *losesTheLock) Save(ctx context.Context, n *node.Node) error {
	if l.failSaves.Add(-1) >= 0 {
		return ports.ErrConcurrentUpdate
	}
	return l.NodeRepository.Save(ctx, n)
}

// TestAConcurrentUpdateLeavesNoKeyRowSoTheRetryExecutes: the loser of an
// optimistic lock answers 409 concurrent-update and is NOT stored; the same
// request under the same key then executes, succeeds and is stored and replayed.
func TestAConcurrentUpdateLeavesNoKeyRowSoTheRetryExecutes(t *testing.T) {
	lock := &losesTheLock{}
	d := newDBAPIWith(t, func(deps *usecases.Deps) {
		lock.NodeRepository = deps.Nodes
		deps.Nodes = lock
	})
	id := activeNodes(t, d, 1)[0]
	outboxBefore := d.count("outbox_events")

	// The use case retries a lost lock once (transact), so two lost Saves are a refusal.
	lock.failSaves.Store(2)
	loser := d.do(http.MethodPost, "/nodes/"+id+"/suspensions", `{"reason":"r"}`, "key-cu")
	if loser.status != http.StatusConflict || loser.slug() != "concurrent-update" {
		t.Fatalf("loser = %d %s, want 409 concurrent-update", loser.status, loser.raw)
	}
	if n := d.keyRows("key-cu"); n != 0 {
		t.Fatalf("key rows after the lost lock = %d, want 0: a retryable refusal must release the key", n)
	}
	if wasReplayed(loser) {
		t.Fatalf("the first answer carries Idempotency-Replayed")
	}

	retry := d.do(http.MethodPost, "/nodes/"+id+"/suspensions", `{"reason":"r"}`, "key-cu")
	if retry.status != http.StatusOK || wasReplayed(retry) || retry.field("status") != "SUSPENDED" {
		t.Fatalf("retry = %d %s replayed=%v, want a fresh 200 SUSPENDED: the key is free, the request runs again", retry.status, retry.raw, wasReplayed(retry))
	}
	if n := d.keyRows("key-cu"); n != 1 {
		t.Fatalf("key rows after the success = %d, want 1: a 2xx is stored", n)
	}
	again := d.do(http.MethodPost, "/nodes/"+id+"/suspensions", `{"reason":"r"}`, "key-cu")
	if again.status != http.StatusOK || !wasReplayed(again) || again.raw != retry.raw {
		t.Fatalf("third = %d %s replayed=%v, want the stored 200 replayed", again.status, again.raw, wasReplayed(again))
	}
	if d.count("outbox_events") != outboxBefore+1 {
		t.Fatalf("outbox rows = %d, want %d: the suspension is published once", d.count("outbox_events"), outboxBefore+1)
	}
}

// lateRegionID mints the one region id the node body names, so the test can
// register the node BEFORE the region exists and create the region afterwards.
type lateRegionID struct{ ids.UUID }

const lateRegion = "region-late"

func (lateRegionID) NewRegionID() region.ID { return lateRegion }

// TestUnknownRegionIsReleasedAndTheSameKeySucceedsOnceTheRegionExists: the
// region a node names may be created by another request, so unknown-region
// (422) is not stored; after the region is created the same key succeeds.
func TestUnknownRegionIsReleasedAndTheSameKeySucceedsOnceTheRegionExists(t *testing.T) {
	d := newDBAPIWith(t, func(deps *usecases.Deps) { deps.IDs = lateRegionID{} })
	body := strings.Replace(registerBody("SORTATION_CENTER", "NETWORK", ""), "region-1", lateRegion, 1)

	refused := d.do(http.MethodPost, "/nodes", body, "key-ur")
	if refused.status != http.StatusUnprocessableEntity || refused.slug() != "unknown-region" {
		t.Fatalf("first = %d %s, want 422 unknown-region", refused.status, refused.raw)
	}
	if n := d.keyRows("key-ur"); n != 0 {
		t.Fatalf("key rows after unknown-region = %d, want 0: the key must be released", n)
	}

	if r := d.do(http.MethodPost, "/regions", `{"code":"LT","name":"Late"}`, "key-region"); r.status != http.StatusCreated || r.field("regionId") != lateRegion {
		t.Fatalf("define region = %d %s, want 201 %s", r.status, r.raw, lateRegion)
	}
	ok := d.do(http.MethodPost, "/nodes", body, "key-ur")
	if ok.status != http.StatusCreated || wasReplayed(ok) {
		t.Fatalf("same key after the region exists = %d %s replayed=%v, want a fresh 201", ok.status, ok.raw, wasReplayed(ok))
	}
	again := d.do(http.MethodPost, "/nodes", body, "key-ur")
	if again.status != http.StatusCreated || !wasReplayed(again) || again.raw != ok.raw {
		t.Fatalf("third = %d %s replayed=%v, want the stored 201 replayed", again.status, again.raw, wasReplayed(again))
	}
	if d.count("nodes") != 1 {
		t.Fatalf("nodes = %d, want 1: the node is registered once", d.count("nodes"))
	}
}

// gatedDefine is a DefineRegion whose FIRST call waits for release and then
// loses the optimistic lock; every later call succeeds. It records how many
// calls ran and the most that ran at once.
type gatedDefine struct {
	calls, running, peak atomic.Int32
	entered, release     chan struct{}
}

func (g *gatedDefine) Execute(context.Context, usecases.DefineRegionCommand) (views.Region, error) {
	n := g.calls.Add(1)
	cur := g.running.Add(1)
	defer g.running.Add(-1)
	for p := g.peak.Load(); cur > p && !g.peak.CompareAndSwap(p, cur); p = g.peak.Load() {
	}
	if n == 1 {
		close(g.entered)
		<-g.release
		return views.Region{}, fmt.Errorf("lost the race: %w", ports.ErrConcurrentUpdate)
	}
	return views.Region{RegionID: "region-9", Code: "SE", Name: "Southeast"}, nil
}

// TestACopyWaitingOnARetryableFirstRunsTheHandlerItself: copies of a request
// that arrive while the first one is in flight wait on the key's unique index.
// The first ends RETRYABLE and rolls back, so the key row is gone: the next
// copy is a fresh request and runs the handler itself (never replays the
// released refusal, never runs alongside the first); the copies behind it
// replay ITS answer.
func TestACopyWaitingOnARetryableFirstRunsTheHandlerItself(t *testing.T) {
	d := newDBAPI(t)
	g := &gatedDefine{entered: make(chan struct{}), release: make(chan struct{})}
	d.srv.DefineRegion = g
	d.api.h = routerOf(t, d.srv)

	const copies = 6
	replies := make([]reply, copies)
	var wg sync.WaitGroup
	start := func(i int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i] = d.do(http.MethodPost, "/regions", regionBody, "key-wait")
		}()
	}
	start(0)
	<-g.entered
	for i := 1; i < copies; i++ {
		start(i)
	}
	time.Sleep(300 * time.Millisecond) // let the copies reach the key's unique index
	close(g.release)
	wg.Wait()

	var refused, fresh, replays int
	for _, r := range replies {
		switch {
		case r.status == http.StatusConflict && r.slug() == "concurrent-update" && !wasReplayed(r):
			refused++
		case r.status == http.StatusCreated && !wasReplayed(r):
			fresh++
		case r.status == http.StatusCreated && wasReplayed(r):
			replays++
		default:
			t.Fatalf("unexpected reply %d %s replayed=%v", r.status, r.raw, wasReplayed(r))
		}
	}
	if refused != 1 || fresh != 1 || replays != copies-2 {
		t.Fatalf("refused %d fresh %d replayed %d, want 1, 1 and %d", refused, fresh, replays, copies-2)
	}
	if g.calls.Load() != 2 || g.peak.Load() != 1 {
		t.Fatalf("handler ran %d times, at most %d at once, want 2 and 1", g.calls.Load(), g.peak.Load())
	}
	if n := d.keyRows("key-wait"); n != 1 {
		t.Fatalf("key rows = %d, want 1 (the stored 201)", n)
	}
}
