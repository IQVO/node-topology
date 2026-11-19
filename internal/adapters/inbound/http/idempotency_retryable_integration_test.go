//go:build integration

package http_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
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

func replayed(r reply) bool { return r.header.Get(inboundhttp.IdempotencyReplayedHeader) == "true" }

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
	if replayed(loser) {
		t.Fatalf("the first answer carries Idempotency-Replayed")
	}

	retry := d.do(http.MethodPost, "/nodes/"+id+"/suspensions", `{"reason":"r"}`, "key-cu")
	if retry.status != http.StatusOK || replayed(retry) || retry.field("status") != "SUSPENDED" {
		t.Fatalf("retry = %d %s replayed=%v, want a fresh 200 SUSPENDED: the key is free, the request runs again", retry.status, retry.raw, replayed(retry))
	}
	if n := d.keyRows("key-cu"); n != 1 {
		t.Fatalf("key rows after the success = %d, want 1: a 2xx is stored", n)
	}
	again := d.do(http.MethodPost, "/nodes/"+id+"/suspensions", `{"reason":"r"}`, "key-cu")
	if again.status != http.StatusOK || !replayed(again) || again.raw != retry.raw {
		t.Fatalf("third = %d %s replayed=%v, want the stored 200 replayed", again.status, again.raw, replayed(again))
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
	if ok.status != http.StatusCreated || replayed(ok) {
		t.Fatalf("same key after the region exists = %d %s replayed=%v, want a fresh 201", ok.status, ok.raw, replayed(ok))
	}
	again := d.do(http.MethodPost, "/nodes", body, "key-ur")
	if again.status != http.StatusCreated || !replayed(again) || again.raw != ok.raw {
		t.Fatalf("third = %d %s replayed=%v, want the stored 201 replayed", again.status, again.raw, replayed(again))
	}
	if d.count("nodes") != 1 {
		t.Fatalf("nodes = %d, want 1: the node is registered once", d.count("nodes"))
	}
}
