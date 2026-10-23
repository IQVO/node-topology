package features_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	httpadapter "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// The REST-level steps drive the REAL chi router (the one cmd/api serves)
// over the same in-memory doubles the use-case steps use, so a scenario can
// assert wire status codes, RFC 9457 problem slugs and response fields of
// apis/openapi.yaml. What needs a live Postgres or Kafka (the transactional
// Idempotency-Key store, the outbox relay) stays in the integration suite;
// here the Idempotency-Key middleware is a pass-through double except in
// http_idempotency.feature, which exercises its request validation (the
// paths that run before the middleware would open a transaction).

// httpWorld is the REST state of one scenario: two routers and the last
// response.
type httpWorld struct {
	api   http.Handler // real router, pass-through Idempotency-Key double
	keyed http.Handler // real router behind the REAL key middleware (nil pool)
	// the last response
	status int
	body   []byte
	doc    any // parsed body, lazily
}

// passthroughIdempotency is the double for the DB-backed Idempotency-Key
// middleware: it lets the scenario focus on the route's own behaviour. The
// real middleware's validation and replay live in http_idempotency.feature
// and in the integration suite.
func passthroughIdempotency(string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// router builds (once per scenario) the real router over the world's doubles.
func (w *world) router(keyed bool) (http.Handler, error) {
	if w.httpw == nil {
		w.httpw = &httpWorld{}
	}
	if keyed && w.httpw.keyed != nil {
		return w.httpw.keyed, nil
	}
	if !keyed && w.httpw.api != nil {
		return w.httpw.api, nil
	}
	idem := httpadapter.Idempotency(passthroughIdempotency)
	if keyed {
		// nil pool: only the pre-store paths (key validation, body limits)
		// may be exercised; a valid key would open a transaction.
		idem = httpadapter.RequireIdempotencyKey(nil, nil)
	}
	s := &httpadapter.Server{
		RegisterNode:       usecases.NewRegisterNode(w.deps),
		DeclareProfile:     usecases.NewDeclareOperationalProfile(w.deps),
		ActivateNode:       usecases.NewActivateNode(w.deps),
		SuspendNode:        usecases.NewSuspendNode(w.deps),
		RetireNode:         usecases.NewRetireNode(w.deps),
		GetNode:            usecases.NewGetNode(w.deps.Nodes),
		ListNodes:          usecases.NewListNodes(w.deps.Nodes),
		OpenLane:           usecases.NewOpenLane(w.deps),
		CloseLane:          usecases.NewCloseLane(w.deps),
		ListLanes:          usecases.NewListLanes(w.deps.Lanes),
		AssignServiceArea:  usecases.NewAssignServiceArea(w.deps),
		ResolveServiceArea: usecases.NewResolveServiceArea(w.deps.ServiceAreas),
		DefineRegion:       usecases.NewDefineRegion(w.deps),
		GetRegion:          usecases.NewGetRegion(w.deps.Regions),
		ListRegions:        usecases.NewListRegions(w.deps.Regions),
		Idempotency:        idem,
		Readiness:          httpadapter.NewReadiness(0, 0),
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h, err := httpadapter.NewRouter(s)
	if err != nil {
		return nil, err
	}
	if keyed {
		w.httpw.keyed = h
	} else {
		w.httpw.api = h
	}
	return h, nil
}

// aliasToken matches node:fc, region:SP, lane:L1 inside a path or an
// expected value; the id it resolves to never contains a colon.
var aliasToken = regexp.MustCompile(`(node|region|lane):([A-Za-z0-9_.-]+)`)

// resolveAliases replaces every node:/region:/lane: alias with its id.
func (w *world) resolveAliases(s string) (string, error) {
	var resolveErr error
	out := aliasToken.ReplaceAllStringFunc(s, func(m string) string {
		parts := strings.SplitN(m, ":", 2)
		id, err := func() (string, error) {
			switch parts[0] {
			case "node":
				id, err := w.nodeID(parts[1])
				return string(id), err
			case "region":
				id, err := w.regionID(parts[1])
				return string(id), err
			default:
				id, err := w.laneID(parts[1])
				return string(id), err
			}
		}()
		if err != nil {
			resolveErr = err
			return m
		}
		return id
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return out, nil
}

// send runs one request through the chosen router and records the response.
func (w *world) send(keyed bool, method, path, key, body string, raw bool) error {
	h, err := w.router(keyed)
	if err != nil {
		return err
	}
	path, err = w.resolveAliases(path)
	if err != nil {
		return err
	}
	body, err = w.resolveAliases(body)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != "" || raw {
		reader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != "" || raw {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set(httpadapter.IdempotencyKeyHeader, key)
	}
	rec := &recordWriter{header: http.Header{}, body: bytes.Buffer{}}
	h.ServeHTTP(rec, req)
	w.httpw.status, w.httpw.body, w.httpw.doc = rec.code, rec.body.Bytes(), nil
	return nil
}

// recordWriter is the client side of the exchange.
type recordWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *recordWriter) Header() http.Header { return r.header }

func (r *recordWriter) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}

func (r *recordWriter) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(b)
}

// parsed decodes the last response body once. Numbers keep their written
// shape (json.Number), so an assertion compares "1200" to 1200.
func (w *world) parsed() (any, error) {
	if w.httpw.doc != nil {
		return w.httpw.doc, nil
	}
	dec := json.NewDecoder(bytes.NewReader(w.httpw.body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("response is not JSON (%q): %w", string(w.httpw.body), err)
	}
	w.httpw.doc = doc
	return doc, nil
}

// lookup walks a dotted path (a.b.0.c) through the decoded response.
func lookup(doc any, path string) (any, bool) {
	if path == "" {
		return doc, true
	}
	cur := doc
	for _, part := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func registerHTTPSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^a (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)"$`, w.httpSend)
	sc.Step(`^a (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the idempotency key "([^"]*)"$`, w.httpSendKeyed)
	sc.Step(`^a (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the JSON body$`, w.httpSendBody)
	sc.Step(`^a (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the idempotency key "([^"]*)" and the JSON body$`, w.httpSendKeyedBody)
	sc.Step(`^a (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the raw body$`, w.httpSendRaw)
	sc.Step(`^a keyed (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)"$`, w.httpSendGuarded)
	sc.Step(`^a keyed (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the JSON body$`, w.httpSendGuardedBody)
	sc.Step(`^a keyed (GET|POST|PUT|PATCH|DELETE) request is sent to "([^"]*)" with the idempotency key "([^"]*)" and the JSON body$`, w.httpSendGuardedKeyedBody)
	sc.Step(`^the response status is (\d+)$`, w.responseStatusIs)
	sc.Step(`^the response problem is "([^"]*)"$`, w.responseProblemIs)
	sc.Step(`^the response field "([^"]*)" is "([^"]*)"$`, w.responseFieldIs)
	sc.Step(`^the response list "([^"]*)" has (-?\d+) items?$`, w.responseListHas)
	sc.Step(`^the response has no field "([^"]*)"$`, w.responseHasNoField)
	sc.Step(`^the node id of the response is remembered as "([^"]*)"$`, w.rememberNodeID)
	sc.Step(`^the lane id of the response is remembered as "([^"]*)"$`, w.rememberLaneID)
	sc.Step(`^the region id of the response is remembered as "([^"]*)"$`, w.rememberRegionID)
}

func (w *world) httpSend(method, path string) error {
	return w.send(false, method, path, "", "", false)
}
func (w *world) httpSendKeyed(method, path, key string) error {
	return w.send(false, method, path, key, "", false)
}
func (w *world) httpSendRaw(method, path string, doc *godog.DocString) error {
	return w.send(false, method, path, "", doc.Content, true)
}
func (w *world) httpSendBody(method, path string, doc *godog.DocString) error {
	return w.send(false, method, path, "", doc.Content, false)
}
func (w *world) httpSendKeyedBody(method, path, key string, doc *godog.DocString) error {
	return w.send(false, method, path, key, doc.Content, false)
}
func (w *world) httpSendGuarded(method, path string) error {
	return w.send(true, method, path, "", "", false)
}
func (w *world) httpSendGuardedBody(method, path string, doc *godog.DocString) error {
	return w.send(true, method, path, "", doc.Content, false)
}
func (w *world) httpSendGuardedKeyedBody(method, path, key string, doc *godog.DocString) error {
	return w.send(true, method, path, key, doc.Content, false)
}

func (w *world) responseStatusIs(want int) error {
	if w.httpw == nil {
		return errNoCommand
	}
	if w.httpw.status != want {
		return fmt.Errorf("response status = %d, want %d (body: %s)", w.httpw.status, want, w.httpw.body)
	}
	return nil
}

func (w *world) responseProblemIs(slug string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, "type")
	if !ok {
		return fmt.Errorf("response has no problem type: %s", w.httpw.body)
	}
	if got != slug {
		return fmt.Errorf("problem type = %v, want %q", got, slug)
	}
	if status, ok := lookup(doc, "status"); ok {
		if n, isNum := number(status); isNum && int(n) != w.httpw.status {
			return fmt.Errorf("problem status = %v, want the HTTP status %d", status, w.httpw.status)
		}
	}
	return nil
}

func (w *world) responseFieldIs(path, want string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, path)
	if !ok {
		return fmt.Errorf("response has no field %q: %s", path, w.httpw.body)
	}
	want, err = w.resolveAliases(want)
	if err != nil {
		return err
	}
	if text(got) != want {
		return fmt.Errorf("response field %q = %v, want %q", path, got, want)
	}
	return nil
}

func (w *world) responseListHas(path string, want int) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, path)
	if !ok {
		return fmt.Errorf("response has no list %q: %s", path, w.httpw.body)
	}
	list, isList := got.([]any)
	if !isList {
		return fmt.Errorf("response field %q is not a list: %v", path, got)
	}
	if len(list) != want {
		return fmt.Errorf("response list %q has %d items, want %d", path, len(list), want)
	}
	return nil
}

func (w *world) responseHasNoField(path string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	if got, ok := lookup(doc, path); ok {
		return fmt.Errorf("response has a field %q (%v), want none", path, got)
	}
	return nil
}

func (w *world) rememberNodeID(alias string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, "nodeId")
	if !ok {
		return fmt.Errorf("response has no nodeId: %s", w.httpw.body)
	}
	id, isString := got.(string)
	if !isString {
		return fmt.Errorf("response nodeId = %v, want a string", got)
	}
	w.nodes[alias] = node.ID(id)
	return nil
}

func (w *world) rememberLaneID(alias string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, "laneId")
	if !ok {
		return fmt.Errorf("response has no laneId: %s", w.httpw.body)
	}
	id, isString := got.(string)
	if !isString {
		return fmt.Errorf("response laneId = %v, want a string", got)
	}
	w.lanes[alias] = lane.ID(id)
	return nil
}

func (w *world) rememberRegionID(alias string) error {
	if w.httpw == nil {
		return errNoCommand
	}
	doc, err := w.parsed()
	if err != nil {
		return err
	}
	got, ok := lookup(doc, "regionId")
	if !ok {
		return fmt.Errorf("response has no regionId: %s", w.httpw.body)
	}
	id, isString := got.(string)
	if !isString {
		return fmt.Errorf("response regionId = %v, want a string", got)
	}
	w.regions[alias] = region.ID(id)
	return nil
}

// text renders a decoded JSON value the way a scenario writes it.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case nil:
		return "<null>"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// number reads a decoded JSON value as a float, when it is one.
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	if err != nil {
		return 0, false
	}
	return f, true
}
