package http_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/region"
)

func newAPI(t *testing.T) *api {
	t.Helper()
	srv, _ := memoryServer()
	return &api{t: t, h: routerOf(t, srv)}
}

func TestNewRouterRefusesAServerWithoutIdempotency(t *testing.T) {
	srv, _ := memoryServer()
	srv.Idempotency = nil
	if _, err := inboundhttp.NewRouter(srv); !errors.Is(err, inboundhttp.ErrNoIdempotency) {
		t.Fatalf("err = %v, want ErrNoIdempotency", err)
	}
}

func TestEveryStateChangingRouteDemandsAnIdempotencyKey(t *testing.T) {
	a := newAPI(t)
	for _, op := range operations {
		if op.method == http.MethodGet {
			continue
		}
		r := a.do(op.method, op.path, op.body, "")
		if r.status != http.StatusBadRequest || r.slug() != "idempotency-key-required" {
			t.Errorf("%s without a key = %d %s, want 400 idempotency-key-required", op.name, r.status, r.raw)
		}
	}
}

func TestReadsNeedNoIdempotencyKey(t *testing.T) {
	a := newAPI(t)
	for _, op := range operations {
		if op.method != http.MethodGet {
			continue
		}
		if r := a.do(op.method, op.path, op.body, ""); r.status == http.StatusBadRequest {
			t.Errorf("%s demanded a key: %s", op.name, r.raw)
		}
	}
}

func TestMalformedBodiesAre400MalformedRequestBody(t *testing.T) {
	a := newAPI(t)
	cases := map[string]string{
		"not json":         `{"code":`,
		"unknown field":    `{"code":"SE","name":"Southeast","colour":"red"}`,
		"wrong type":       `{"code":1,"name":"Southeast"}`,
		"trailing data":    `{"code":"SE","name":"Southeast"} {"x":1}`,
		"an array":         `[]`,
		"empty body":       ``,
		"a bare string":    `"SE"`,
		"object for text":  `{"code":{"a":1},"name":"x"}`,
		"null body object": `{"code":null,"name":null,"extra":null}`,
	}
	for name, body := range cases {
		r := a.keyed(http.MethodPost, "/regions", body)
		if r.status != http.StatusBadRequest || r.slug() != "malformed-request-body" || !r.isProblem() {
			t.Errorf("%s: %d %s, want 400 malformed-request-body", name, r.status, r.raw)
		}
	}
}

func TestAnOversizedBodyIs413(t *testing.T) {
	a := newAPI(t)
	r := a.keyed(http.MethodPost, "/regions", `{"code":"SE","name":"`+strings.Repeat("x", 70<<10)+`"}`)
	if r.status != http.StatusRequestEntityTooLarge || r.slug() != "request-body-too-large" {
		t.Fatalf("oversized body = %d %.120s", r.status, r.raw)
	}
}

func TestNULIsRejectedBeforeAnythingElse(t *testing.T) {
	a := newAPI(t)
	cases := []struct {
		name                  string
		method                string
		path, rawPath, rawQry string
		body, key             string
		status                int
		slug                  string
	}{
		{"raw NUL in the path", http.MethodGet, "/nodes/no\x00de", "", "", "", "", 404, "not-found"},
		{"encoded NUL in the path", http.MethodGet, "/nodes/no\x00de", "/nodes/no%00de", "", "", "", 404, "not-found"},
		{"encoded NUL in a keyed path", http.MethodPost, "/nodes/no\x00de:activate", "/nodes/no%00de:activate", "", "", "k1", 404, "not-found"},
		{"NUL in the query", http.MethodGet, "/nodes", "", "type=%00", "", "", 400, "malformed-request-query"},
		{"raw NUL in the query", http.MethodGet, "/nodes", "", "type=a\x00b", "", "", 400, "malformed-request-query"},
		{"escaped NUL in a body", http.MethodPost, "/regions", "", "", `{"code":"S\u0000E","name":"x"}`, "k2", 400, "malformed-request-body"},
		{"raw NUL in a body", http.MethodPost, "/regions", "", "", "{\"code\":\"S\x00E\",\"name\":\"x\"}", "k3", 400, "malformed-request-body"},
		{"NUL before a missing key", http.MethodPost, "/regions", "", "", `{"code":"S\u0000E","name":"x"}`, "", 400, "malformed-request-body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A NUL cannot be parsed out of a URL string, so the request URL is built field by field.
			req := httptest.NewRequest(tc.method, "/", strings.NewReader(tc.body))
			req.URL = &url.URL{Path: tc.path, RawPath: tc.rawPath, RawQuery: tc.rawQry}
			if tc.key != "" {
				req.Header.Set(inboundhttp.IdempotencyKeyHeader, tc.key)
			}
			rec := httptest.NewRecorder()
			a.h.ServeHTTP(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"type":"`+tc.slug+`"`) {
				t.Fatalf("%d %s, want %d %s", rec.Code, rec.Body.String(), tc.status, tc.slug)
			}
		})
	}
	if list := a.get("/regions"); trimNL(list.raw) != "[]" {
		t.Fatalf("a rejected NUL request created something: %s", list.raw)
	}
}

func TestAnEscapedBackslashBeforeU0000IsNotANUL(t *testing.T) {
	a := newAPI(t)
	r := a.keyed(http.MethodPost, "/regions", `{"code":"SE","name":"a\\u0000b"}`)
	if r.status != http.StatusCreated {
		t.Fatalf("an escaped backslash followed by u0000 is plain text, got %d %s", r.status, r.raw)
	}
}

func TestUnknownRoutesAndMethodsAreProblems(t *testing.T) {
	a := newAPI(t)
	if r := a.get("/nope"); r.status != http.StatusNotFound || r.slug() != "not-found" || !r.isProblem() {
		t.Fatalf("unknown route = %d %s", r.status, r.raw)
	}
	if r := a.do(http.MethodDelete, "/nodes/node-1", "", "k"); r.status != http.StatusMethodNotAllowed || r.slug() != "method-not-allowed" {
		t.Fatalf("unsupported method = %d %s", r.status, r.raw)
	}
}

func TestHealthzIsLivenessAndAlwaysOK(t *testing.T) {
	srv, _ := memoryServer()
	srv.Readiness = inboundhttp.NewReadiness(0, 0, inboundhttp.Dependency{Name: "postgres", Check: func(context.Context) error { return errors.New("down") }})
	srv.Readiness.SetNotReady()
	a := &api{t: t, h: routerOf(t, srv)}
	if r := a.get("/healthz"); r.status != http.StatusOK || trimNL(r.raw) != `{"status":"ok"}` {
		t.Fatalf("/healthz = %d %s while draining and failing, want 200 ok", r.status, r.raw)
	}
}

func TestReadyzStates(t *testing.T) {
	var postgresDown, kafkaDown bool
	deps := []inboundhttp.Dependency{
		{Name: "postgres", Check: func(context.Context) error {
			if postgresDown {
				return errors.New("down")
			}
			return nil
		}},
		{Name: "kafka", Check: func(context.Context) error {
			if kafkaDown {
				return errors.New("down")
			}
			return nil
		}},
	}
	ready := func() *api {
		srv, _ := memoryServer()
		srv.Readiness = inboundhttp.NewReadiness(time.Nanosecond, time.Second, deps...)
		return &api{t: t, h: routerOf(t, srv)}
	}
	a := ready()
	if r := a.get("/readyz"); r.status != 200 || trimNL(r.raw) != `{"status":"ready"}` {
		t.Fatalf("healthy /readyz = %d %s", r.status, r.raw)
	}
	kafkaDown = true
	time.Sleep(2 * time.Millisecond)
	r := a.get("/readyz")
	if r.status != 503 || r.slug() != "dependency-unavailable" || !r.isProblem() || !strings.Contains(r.field("detail"), "kafka") || strings.Contains(r.field("detail"), "postgres") {
		t.Fatalf("kafka down = %d %s, want 503 dependency-unavailable naming only kafka", r.status, r.raw)
	}
	postgresDown = true
	time.Sleep(2 * time.Millisecond)
	if r := a.get("/readyz"); !strings.Contains(r.field("detail"), "postgres, kafka") {
		t.Fatalf("both down = %s, want both named", r.raw)
	}
	postgresDown, kafkaDown = false, false
	time.Sleep(2 * time.Millisecond)
	if r := a.get("/readyz"); r.status != 200 {
		t.Fatalf("recovered /readyz = %d %s", r.status, r.raw)
	}
}

func TestReadyzDrainGateBeatsHealthyDependencies(t *testing.T) {
	srv, _ := memoryServer()
	a := &api{t: t, h: routerOf(t, srv)}
	srv.Readiness.SetNotReady()
	if r := a.get("/readyz"); r.status != 503 || trimNL(r.raw) != `{"status":"not_ready"}` {
		t.Fatalf("draining /readyz = %d %s, want 503 not_ready", r.status, r.raw)
	}
}

func TestReadinessVerdictIsCachedAndDetachedFromTheCaller(t *testing.T) {
	calls := 0
	g := inboundhttp.NewReadiness(time.Hour, time.Second, inboundhttp.Dependency{Name: "postgres", Check: func(ctx context.Context) error {
		calls++
		return ctx.Err()
	}})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := g.Failing(cancelled); len(got) != 0 {
		t.Fatalf("a hung-up caller poisoned the verdict: %v", got)
	}
	g.Failing(context.Background())
	if calls != 1 {
		t.Fatalf("the verdict was not cached: %d checks", calls)
	}
	var nilGate *inboundhttp.Readiness
	nilGate.SetNotReady()
	if !nilGate.Ready() || nilGate.Failing(context.Background()) != nil {
		t.Fatal("a nil gate is ready with no dependency")
	}
}

func TestAPanicIsA500ProblemAndTheServerKeepsServing(t *testing.T) {
	srv, _ := memoryServer()
	srv.GetRegion = panicking{}
	var logs bytes.Buffer
	srv.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := &api{t: t, h: routerOf(t, srv)}
	r := a.get("/regions/region-1")
	if r.status != 500 || r.slug() != "internal-error" || strings.Contains(r.raw, "boom") {
		t.Fatalf("panic = %d %s, want an opaque 500", r.status, r.raw)
	}
	if r := a.get("/healthz"); r.status != 200 {
		t.Fatalf("the server stopped serving: %d", r.status)
	}
	if !strings.Contains(logs.String(), "status=500") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("the 500 was not logged at ERROR: %s", logs.String())
	}
	if strings.Contains(logs.String(), "healthz") {
		t.Fatalf("probes must not be logged (ADR 0010): %s", logs.String())
	}
}

type panicking struct{}

func (panicking) Execute(context.Context, region.ID) (views.Region, error) { panic("boom") }
