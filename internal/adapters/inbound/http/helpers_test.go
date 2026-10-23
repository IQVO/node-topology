package http_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/adapters/outbound/memory"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
)

// requireKeyOnly stands in for the Postgres-backed Idempotency-Key middleware
// in these unit tests: it enforces only that a key is present. The real
// middleware (replay, conflict, expiry, rollback) is proven against a real
// database in idempotency_integration_test.go.
func requireKeyOnly(string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(inboundhttp.IdempotencyKeyHeader) == "" {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"type":"idempotency-key-required","title":"Idempotency-Key header is required","status":400}`)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// memoryServer wires the real use cases over the in-memory doubles.
func memoryServer() (*inboundhttp.Server, *memory.Store) {
	s := memory.NewStore()
	d := usecases.Deps{
		Nodes: s.Nodes(), Lanes: s.Lanes(), ServiceAreas: s.ServiceAreas(), Regions: s.Regions(), Outbox: s.Outbox(),
		UoW: s.UnitOfWork(), Clock: memory.NewClock(repocontract.Now), IDs: &memory.SequentialIDs{},
	}
	return &inboundhttp.Server{
		RegisterNode: usecases.NewRegisterNode(d), DeclareProfile: usecases.NewDeclareOperationalProfile(d),
		ActivateNode: usecases.NewActivateNode(d), SuspendNode: usecases.NewSuspendNode(d), RetireNode: usecases.NewRetireNode(d),
		GetNode: usecases.NewGetNode(d.Nodes), ListNodes: usecases.NewListNodes(d.Nodes),
		OpenLane: usecases.NewOpenLane(d), CloseLane: usecases.NewCloseLane(d), ListLanes: usecases.NewListLanes(d.Lanes),
		AssignServiceArea: usecases.NewAssignServiceArea(d), ResolveServiceArea: usecases.NewResolveServiceArea(d.ServiceAreas),
		DefineRegion: usecases.NewDefineRegion(d), GetRegion: usecases.NewGetRegion(d.Regions), ListRegions: usecases.NewListRegions(d.Regions),
		Idempotency: requireKeyOnly, Readiness: inboundhttp.NewReadiness(0, 0),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, s
}

func routerOf(t *testing.T, s *inboundhttp.Server) http.Handler {
	t.Helper()
	h, err := inboundhttp.NewRouter(s)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return h
}

// api drives a router over httptest.
type api struct {
	t *testing.T
	h http.Handler
	n int
}

type reply struct {
	status  int
	header  http.Header
	raw     string
	decoded any
}

// do sends one request. A non-empty key is sent as the Idempotency-Key.
func (a *api) do(method, path, body, key string) reply {
	a.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set(inboundhttp.IdempotencyKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	out := reply{status: rec.Code, header: rec.Header(), raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.decoded)
	return out
}

// keyed sends a state-changing request with a fresh Idempotency-Key.
func (a *api) keyed(method, path, body string) reply {
	a.t.Helper()
	a.n++
	return a.do(method, path, body, fmt.Sprintf("key-%s-%d", method, a.n))
}

// get reads a path.
func (a *api) get(path string) reply { a.t.Helper(); return a.do(http.MethodGet, path, "", "") }

// field returns the JSON object member of the reply, "" when absent.
func (r reply) field(name string) string {
	m, ok := r.decoded.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := m[name].(string); ok {
		return v
	}
	return ""
}

// slug returns the problem type of a problem+json reply.
func (r reply) slug() string { return r.field("type") }

func (r reply) isProblem() bool {
	return strings.HasPrefix(r.header.Get("Content-Type"), "application/problem+json")
}
