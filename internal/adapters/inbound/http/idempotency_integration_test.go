//go:build integration

package http_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/adapters/outbound/clock"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres/pgtx"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

// These tests run the REAL Idempotency-Key middleware and the REAL use cases
// over a real Postgres (testcontainers). The router is the production one.

type dbAPI struct {
	*api
	pool *pgtest.Pool
	srv  *inboundhttp.Server
}

func newDBAPI(t *testing.T) dbAPI {
	t.Helper()
	pool := pgtest.NewPool(t)
	outbox := postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder())
	d := usecases.Deps{
		Nodes: postgres.NewNodeRepo(pool), Lanes: postgres.NewLaneRepo(pool), ServiceAreas: postgres.NewServiceAreaRepo(pool),
		Regions: postgres.NewRegionRepo(pool), Outbox: outbox, UoW: postgres.NewUnitOfWork(pool), Clock: clock.System{}, IDs: ids.UUID{},
	}
	srv := &inboundhttp.Server{
		RegisterNode: usecases.NewRegisterNode(d), DeclareProfile: usecases.NewDeclareOperationalProfile(d),
		ActivateNode: usecases.NewActivateNode(d), SuspendNode: usecases.NewSuspendNode(d), RetireNode: usecases.NewRetireNode(d),
		GetNode: usecases.NewGetNode(d.Nodes), ListNodes: usecases.NewListNodes(d.Nodes),
		OpenLane: usecases.NewOpenLane(d), CloseLane: usecases.NewCloseLane(d), ListLanes: usecases.NewListLanes(d.Lanes),
		AssignServiceArea: usecases.NewAssignServiceArea(d), ResolveServiceArea: usecases.NewResolveServiceArea(d.ServiceAreas),
		DefineRegion: usecases.NewDefineRegion(d), GetRegion: usecases.NewGetRegion(d.Regions), ListRegions: usecases.NewListRegions(d.Regions),
		Idempotency: inboundhttp.RequireIdempotencyKey(pool, pgtx.With), Readiness: inboundhttp.NewReadiness(0, 0),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return dbAPI{api: &api{t: t, h: routerOf(t, srv)}, pool: pool, srv: srv}
}

func (d dbAPI) count(table string) int {
	d.t.Helper()
	var n int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		d.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

const regionBody = `{"code":"SE","name":"Southeast"}`

func TestTheSameKeyAndBodyReplaysTheFirstResponse(t *testing.T) {
	d := newDBAPI(t)
	first := d.do(http.MethodPost, "/regions", regionBody, "key-1")
	second := d.do(http.MethodPost, "/regions", regionBody, "key-1")

	if first.status != http.StatusCreated || second.status != first.status || second.raw != first.raw {
		t.Fatalf("first %d %s, replay %d %s: want the identical response", first.status, first.raw, second.status, second.raw)
	}
	if first.header.Get(inboundhttp.IdempotencyReplayedHeader) != "" || second.header.Get(inboundhttp.IdempotencyReplayedHeader) != "true" {
		t.Fatalf("replayed headers: first %q second %q", first.header.Get(inboundhttp.IdempotencyReplayedHeader), second.header.Get(inboundhttp.IdempotencyReplayedHeader))
	}
	if second.header.Get("Content-Type") != "application/json" {
		t.Fatalf("replay content type = %q", second.header.Get("Content-Type"))
	}
	if d.count("regions") != 1 || d.count("outbox_events") != 1 || d.count("idempotency_keys") != 1 {
		t.Fatalf("rows: regions %d outbox %d keys %d, want 1 each", d.count("regions"), d.count("outbox_events"), d.count("idempotency_keys"))
	}
}

func TestTheSameKeyWithAnotherBodyIs422KeyReused(t *testing.T) {
	d := newDBAPI(t)
	d.do(http.MethodPost, "/regions", regionBody, "key-1")
	r := d.do(http.MethodPost, "/regions", `{"code":"NE","name":"Northeast"}`, "key-1")
	if r.status != http.StatusUnprocessableEntity || r.slug() != "idempotency-key-reused" || !r.isProblem() {
		t.Fatalf("conflicting body = %d %s, want 422 idempotency-key-reused", r.status, r.raw)
	}
	if d.count("regions") != 1 {
		t.Fatalf("the conflicting request created a region")
	}
}

func TestTheSameKeyOnAnotherResourceIsAnotherRequest(t *testing.T) {
	d := newDBAPI(t)
	w := activeNodes(t, d, 2)
	first := d.do(http.MethodPost, "/nodes/"+w[0]+":suspend", `{"reason":"r"}`, "same-key")
	if first.status != http.StatusOK {
		t.Fatalf("suspend = %d %s", first.status, first.raw)
	}
	other := d.do(http.MethodPost, "/nodes/"+w[1]+":suspend", `{"reason":"r"}`, "same-key")
	if other.status != http.StatusUnprocessableEntity || other.slug() != "idempotency-key-reused" {
		t.Fatalf("same key, other node = %d %s, want 422 idempotency-key-reused", other.status, other.raw)
	}
	if n := d.get("/nodes/" + w[1]); n.field("status") != "ACTIVE" {
		t.Fatalf("the refused request suspended the other node: %s", n.raw)
	}
}

func activeNodes(t *testing.T, d dbAPI, n int) []string {
	t.Helper()
	d.do(http.MethodPost, "/regions", `{"code":"SE","name":"Southeast"}`, "region-key")
	reg := d.get("/regions").decoded.([]any)[0].(map[string]any)["regionId"].(string)
	var out []string
	for i := range n {
		body := strings.Replace(registerBody("SORTATION_CENTER", "NETWORK", ""), "region-1", reg, 1)
		r := d.do(http.MethodPost, "/nodes", body, "reg-"+string(rune('a'+i)))
		id := r.field("nodeId")
		if r.status != http.StatusCreated {
			t.Fatalf("register: %d %s", r.status, r.raw)
		}
		d.do(http.MethodPut, "/nodes/"+id+"/operational-profile", profileWith(`,"sortDwellMinutes":10`), "prof-"+string(rune('a'+i)))
		if a := d.do(http.MethodPost, "/nodes/"+id+":activate", "", "act-"+string(rune('a'+i))); a.status != http.StatusOK {
			t.Fatalf("activate: %d %s", a.status, a.raw)
		}
		out = append(out, id)
	}
	return out
}

func TestAMissingOrInvalidKeyIs400(t *testing.T) {
	d := newDBAPI(t)
	if r := d.do(http.MethodPost, "/regions", regionBody, ""); r.status != 400 || r.slug() != "idempotency-key-required" {
		t.Fatalf("no key = %d %s", r.status, r.raw)
	}
	if r := d.do(http.MethodPost, "/regions", regionBody, strings.Repeat("k", 201)); r.status != 400 || r.slug() != "idempotency-key-invalid" {
		t.Fatalf("201-char key = %d %s", r.status, r.raw)
	}
	if r := d.do(http.MethodPost, "/regions", regionBody, strings.Repeat("k", 200)); r.status != 201 {
		t.Fatalf("200-char key = %d %s, want it accepted", r.status, r.raw)
	}
	if d.count("idempotency_keys") != 1 {
		t.Fatalf("refused keys were recorded: %d rows", d.count("idempotency_keys"))
	}
}

// TestA4xxIsStoredAndItsWritesAreRolledBack: the refusal replays, and nothing
// the failed handler wrote survives.
func TestA4xxIsStoredReplayedAndLeavesNothingBehind(t *testing.T) {
	d := newDBAPI(t)
	body := `{"nodeType":"SORTATION_CENTER","operator":"NETWORK","regionId":"region-nope","timezone":"America/Sao_Paulo","location":{"postalCode":"1","countryCode":"BR"}}`
	first := d.do(http.MethodPost, "/nodes", body, "key-1")
	second := d.do(http.MethodPost, "/nodes", body, "key-1")
	if first.status != 422 || first.slug() != "unknown-region" || second.status != 422 || second.raw != first.raw {
		t.Fatalf("first %d %s replay %d %s", first.status, first.raw, second.status, second.raw)
	}
	if second.header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("a replayed problem lost its content type: %q", second.header.Get("Content-Type"))
	}
	if d.count("nodes") != 0 || d.count("outbox_events") != 0 {
		t.Fatalf("nodes %d outbox %d, want none", d.count("nodes"), d.count("outbox_events"))
	}
	var status int
	if err := d.pool.QueryRow(context.Background(), `SELECT response_status FROM idempotency_keys WHERE key = 'key-1'`).Scan(&status); err != nil || status != 422 {
		t.Fatalf("stored status = %d (%v), want 422", status, err)
	}
}

type boom struct{ calls *int }

func (b boom) Execute(context.Context, usecases.DefineRegionCommand) (views.Region, error) {
	*b.calls++
	if *b.calls == 1 {
		return views.Region{}, errors.New("pq: connection reset")
	}
	return views.Region{RegionID: "region-9", Code: "SE", Name: "Southeast"}, nil
}

// TestA5xxIsNeverStored: the same key can retry the real work.
func TestA5xxIsNeverStoredSoTheKeyCanRetry(t *testing.T) {
	d := newDBAPI(t)
	calls := 0
	d.srv.DefineRegion = boom{&calls}
	d.api.h = routerOf(t, d.srv)

	first := d.do(http.MethodPost, "/regions", regionBody, "key-1")
	if first.status != 500 || d.count("idempotency_keys") != 0 {
		t.Fatalf("500 = %d, keys stored %d, want a 500 and no key", first.status, d.count("idempotency_keys"))
	}
	second := d.do(http.MethodPost, "/regions", regionBody, "key-1")
	if second.status != http.StatusCreated || calls != 2 || second.header.Get(inboundhttp.IdempotencyReplayedHeader) != "" {
		t.Fatalf("retry = %d after %d calls, want a fresh 201", second.status, calls)
	}
}

func TestAnExpiredKeyIsANewKey(t *testing.T) {
	d := newDBAPI(t)
	d.do(http.MethodPost, "/regions", regionBody, "key-1")
	if _, err := d.pool.Exec(context.Background(), `UPDATE idempotency_keys SET created_at = now() - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	r := d.do(http.MethodPost, "/regions", regionBody, "key-1")
	if r.status != http.StatusConflict || r.slug() != "region-code-taken" {
		t.Fatalf("after 24 h the key is new: the second POST runs and meets the existing code, got %d %s", r.status, r.raw)
	}
	if d.count("idempotency_keys") != 1 {
		t.Fatalf("the expired row was not purged: %d rows", d.count("idempotency_keys"))
	}
	if _, err := d.pool.Exec(context.Background(), `UPDATE idempotency_keys SET created_at = now() - interval '23 hours'`); err != nil {
		t.Fatal(err)
	}
	if again := d.do(http.MethodPost, "/regions", regionBody, "key-1"); again.header.Get(inboundhttp.IdempotencyReplayedHeader) != "true" {
		t.Fatalf("a 23 hour old key must still replay: %d %s", again.status, again.raw)
	}
}

func TestConcurrentRequestsWithOneKeyDoTheWorkOnce(t *testing.T) {
	d := newDBAPI(t)
	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		replies []reply
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := d.do(http.MethodPost, "/regions", regionBody, "key-1")
			mu.Lock()
			replies = append(replies, r)
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, r := range replies {
		if r.status != http.StatusCreated || r.raw != replies[0].raw {
			t.Fatalf("replies differ: %d %s vs %s", r.status, r.raw, replies[0].raw)
		}
	}
	if d.count("regions") != 1 || d.count("outbox_events") != 1 {
		t.Fatalf("regions %d outbox %d, want the work done once", d.count("regions"), d.count("outbox_events"))
	}
}

func TestAHandlerPanicRollsTheKeyBack(t *testing.T) {
	d := newDBAPI(t)
	d.srv.DefineRegion = panicOnce{}
	d.api.h = routerOf(t, d.srv)
	if r := d.do(http.MethodPost, "/regions", regionBody, "key-1"); r.status != 500 {
		t.Fatalf("panic = %d %s", r.status, r.raw)
	}
	if d.count("idempotency_keys") != 0 {
		t.Fatalf("a panicking request left its key behind")
	}
}

type panicOnce struct{}

func (panicOnce) Execute(context.Context, usecases.DefineRegionCommand) (views.Region, error) {
	panic("boom")
}
