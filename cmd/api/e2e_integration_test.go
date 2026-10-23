//go:build integration

package main

// The real-wiring test boots run(ctx), the production entry point, against a
// real Postgres and a real Kafka broker (both testcontainers), drives it ONLY
// through REST and reads the result ONLY from Postgres and from the output
// topic. Nothing here is a double: the use cases, the Postgres repositories,
// the Idempotency-Key middleware, the outbox relay, the encoders and the Kafka
// writer are the ones the binary runs.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/kafka/cloudevents"
	"github.com/claudioed/node-topology/internal/testing/kafkatest"
	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

const eventsTopic = "retailnetwork.node-topology.events"

// service is the running binary under test.
type service struct {
	t      *testing.T
	base   string
	pool   *pgtest.Pool
	cancel context.CancelFunc
	done   chan error
	keys   int
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// boot starts run(ctx) on a fresh database (no migration applied: the binary
// migrates at boot) and waits until /healthz answers.
func boot(t *testing.T) *service {
	t.Helper()
	dbURL := pgtest.EmptyURL(t)
	addr := freeAddr(t)
	t.Setenv("DATABASE_URL", dbURL)
	kafkatest.SetBrokersEnv(t)
	t.Setenv("HTTP_ADDR", addr)
	t.Setenv("SHUTDOWN_DRAIN_DELAY", "0")
	t.Setenv("OUTBOX_RELAY_INTERVAL", "100ms")
	t.Setenv("LOG_LEVEL", "error")

	ctx, cancel := context.WithCancel(context.Background())
	s := &service{t: t, base: "http://" + addr, cancel: cancel, done: make(chan error, 1)}
	go func() { s.done <- run(ctx) }()
	t.Cleanup(func() { cancel(); s.waitStopped(30 * time.Second) })

	s.eventually(60*time.Second, "healthz", func() bool { return s.get("/healthz").status == http.StatusOK })
	s.pool = pgtest.OpenPool(t, dbURL)
	return s
}

func (s *service) waitStopped(timeout time.Duration) error {
	select {
	case err := <-s.done:
		s.done <- err
		return err
	case <-time.After(timeout):
		s.t.Errorf("run(ctx) did not return within %s of the cancellation", timeout)
		return nil
	}
}

func (s *service) eventually(timeout time.Duration, what string, ok func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *service) call(method, path, body, key string) response {
	s.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return response{status: -1, body: []byte(err.Error())}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, header: resp.Header, body: raw}
}

func (s *service) get(path string) response { return s.call(http.MethodGet, path, "", "") }

// write sends a state-changing request under a fresh key and requires the
// status.
func (s *service) write(method, path, body string, want int) map[string]any {
	s.t.Helper()
	s.keys++
	r := s.call(method, path, body, fmt.Sprintf("e2e-%d", s.keys))
	if r.status != want {
		s.t.Fatalf("%s %s = %d %s, want %d", method, path, r.status, r.body, want)
	}
	var out map[string]any
	_ = json.Unmarshal(r.body, &out)
	return out
}

func (s *service) count(table string) int {
	s.t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		s.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

const profile = `{"profileRevision":1,"timezone":"America/Sao_Paulo","operatingHoursByDay":{"monday":{"start":"06:00","end":"22:00"},"tuesday":{"start":"06:00","end":"22:00"}},` +
	`"holidays":["2026-12-25"],"handlingTimeMinutes":45,"cutoffs":[{"localTime":"14:00","days":["monday"],"maxOrders":50},{"localTime":"18:00"}],` +
	`"throughputCap":{"value":300,"timeUnit":"HOUR","unit":"ORDER"},"declaredBy":"%s"%s}`

func location(extra string) string {
	return `,"timezone":"America/Sao_Paulo","location":{"postalCode":"01310-100","countryCode":"BR","lat":-23.56,"lon":-46.65}` + extra
}

// world is what the REST flow created.
type world struct{ region, fc, sc, station, lane string }

func TestRealWiring(t *testing.T) {
	s := boot(t)

	t.Run("ready while the output topic does not exist", func(t *testing.T) {
		r := s.get("/readyz")
		if r.status != http.StatusOK || strings.TrimSpace(string(r.body)) != `{"status":"ready"}` {
			t.Fatalf("/readyz = %d %s, want 200 ready", r.status, r.body)
		}
		if n := kafkatest.Count(t, eventsTopic); n != 0 {
			t.Fatalf("the topic already holds %d messages before the first write", n)
		}
	})

	var w world
	t.Run("REST flow", func(t *testing.T) { w = restFlow(t, s) })
	t.Run("Postgres rows", func(t *testing.T) { assertRows(t, s) })
	t.Run("idempotent replay", func(t *testing.T) { assertReplay(t, s) })
	t.Run("exact CloudEvents on the topic", func(t *testing.T) { assertTopic(t, s, w) })
	t.Run("Kafka down", func(t *testing.T) { assertKafkaDown(t, s) })
}

func restFlow(t *testing.T, s *service) world {
	t.Helper()
	var w world
	w.region = str(s.write(http.MethodPost, "/regions", `{"code":"SE","name":"Southeast"}`, http.StatusCreated), "regionId")

	fc := s.write(http.MethodPost, "/nodes", `{"nodeType":"FULFILLMENT_CENTER","subtype":"SORTABLE","operator":"VENDOR","vendorRef":{"vendorId":"vendor-1","vendorNodeCode":"FC-A"},"regionId":"`+w.region+`"`+location("")+`}`, http.StatusCreated)
	sc := s.write(http.MethodPost, "/nodes", `{"nodeType":"SORTATION_CENTER","operator":"NETWORK","regionId":"`+w.region+`"`+location("")+`}`, http.StatusCreated)
	st := s.write(http.MethodPost, "/nodes", `{"nodeType":"DELIVERY_STATION","operator":"NETWORK","regionId":"`+w.region+`"`+location("")+`}`, http.StatusCreated)
	w.fc, w.sc, w.station = str(fc, "nodeId"), str(sc, "nodeId"), str(st, "nodeId")
	if str(fc, "status") != "PROPOSED" || str(fc, "operator") != "VENDOR" {
		t.Fatalf("registered FC = %v", fc)
	}

	s.write(http.MethodPut, "/nodes/"+w.fc+"/operational-profile", fmt.Sprintf(profile, "VENDOR", ""), http.StatusOK)
	s.write(http.MethodPut, "/nodes/"+w.sc+"/operational-profile", fmt.Sprintf(profile, "NETWORK_OPS", `,"sortDwellMinutes":25`), http.StatusOK)
	s.write(http.MethodPut, "/nodes/"+w.station+"/operational-profile", fmt.Sprintf(profile, "NETWORK_OPS", ""), http.StatusOK)
	for _, id := range []string{w.fc, w.sc, w.station} {
		if n := s.write(http.MethodPost, "/nodes/"+id+":activate", "", http.StatusOK); str(n, "status") != "ACTIVE" {
			t.Fatalf("activate %s = %v", id, n)
		}
	}

	l := s.write(http.MethodPost, "/lanes", `{"originNodeId":"`+w.fc+`","destinationNodeId":"`+w.sc+`","mode":"GROUND","nominalTransitMinutes":120}`, http.StatusCreated)
	w.lane = str(l, "laneId")
	area := s.write(http.MethodPut, "/service-areas/01310", `{"deliveryStationNodeId":"`+w.station+`","assignmentVersion":1}`, http.StatusOK)
	if str(area, "regionId") != w.region {
		t.Fatalf("service area = %v", area)
	}

	got := s.get("/nodes/" + w.sc)
	var sc2 map[string]any
	_ = json.Unmarshal(got.body, &sc2)
	if got.status != 200 || str(sc2, "status") != "ACTIVE" || sc2["operationalProfile"] == nil {
		t.Fatalf("GET node = %d %s", got.status, got.body)
	}
	if r := s.get("/service-areas/01310"); r.status != 200 || !strings.Contains(string(r.body), w.station) {
		t.Fatalf("GET service area = %d %s", r.status, r.body)
	}
	if r := s.get("/regions/" + w.region); r.status != 200 {
		t.Fatalf("GET region = %d %s", r.status, r.body)
	}
	if r := s.get("/lanes?origin=" + w.fc); r.status != 200 || !strings.Contains(string(r.body), w.lane) {
		t.Fatalf("GET lanes = %d %s", r.status, r.body)
	}
	return w
}

func assertRows(t *testing.T, s *service) {
	t.Helper()
	want := map[string]int{
		"regions": 1, "nodes": 3, "node_profile_revisions": 3, "lanes": 1, "service_areas": 1,
		"outbox_events":    12, // 1 region + 3 registered + 3 profiles + 3 activated + 1 lane + 1 service area
		"idempotency_keys": 12, // the 12 keyed writes above
	}
	for table, n := range want {
		if got := s.count(table); got != n {
			t.Errorf("%s holds %d rows, want %d", table, got, n)
		}
	}
	var status string
	var vendorCode *string
	if err := s.pool.QueryRow(context.Background(), `SELECT status, vendor_node_code FROM nodes WHERE vendor_node_code = 'FC-A'`).Scan(&status, &vendorCode); err != nil || status != "ACTIVE" {
		t.Errorf("vendor FC row: status %q err %v", status, err)
	}
}

func assertReplay(t *testing.T, s *service) {
	t.Helper()
	body := `{"code":"NE","name":"Northeast"}`
	first := s.call(http.MethodPost, "/regions", body, "replay-1")
	second := s.call(http.MethodPost, "/regions", body, "replay-1")
	if first.status != http.StatusCreated || second.status != first.status || !bytes.Equal(first.body, second.body) {
		t.Fatalf("first %d %s, replay %d %s", first.status, first.body, second.status, second.body)
	}
	if second.header.Get("Idempotency-Replayed") != "true" {
		t.Fatal("the replay is not marked")
	}
	if n := s.count("regions"); n != 2 {
		t.Fatalf("regions = %d after a replayed create, want 2", n)
	}
	if n := s.count("outbox_events"); n != 13 {
		t.Fatalf("outbox rows = %d after a replayed create, want 13 (one more event)", n)
	}
	if r := s.call(http.MethodPost, "/regions", `{"code":"XX","name":"Other"}`, "replay-1"); r.status != http.StatusUnprocessableEntity ||
		!strings.Contains(string(r.body), "idempotency-key-reused") {
		t.Fatalf("conflicting body = %d %s, want 422 idempotency-key-reused", r.status, r.body)
	}
	if r := s.call(http.MethodPost, "/regions", body, ""); r.status != http.StatusBadRequest {
		t.Fatalf("no key = %d, want 400", r.status)
	}
}

type outboxRow struct {
	id                 int64
	eventID, typ, subj string
	value              []byte
}

func outboxRows(t *testing.T, s *service) []outboxRow {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), `SELECT id, event_id, event_type, subject, value FROM outbox_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.eventID, &r.typ, &r.subj, &r.value); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func assertTopic(t *testing.T, s *service, w world) {
	t.Helper()
	rows := outboxRows(t, s)
	got := kafkatest.ReadN(t, eventsTopic, len(rows), 90*time.Second)

	wantTypes := []string{
		"region.RegionDefined",
		"node.NodeRegistered", "node.NodeRegistered", "node.NodeRegistered",
		"node.NodeOperationalProfileDeclared", "node.NodeOperationalProfileDeclared", "node.NodeOperationalProfileDeclared",
		"node.NodeActivated", "node.NodeActivated", "node.NodeActivated",
		"lane.LaneOpened", "servicearea.ServiceAreaAssigned", "region.RegionDefined",
	}
	if len(rows) != len(wantTypes) {
		t.Fatalf("outbox holds %d rows, want %d", len(rows), len(wantTypes))
	}
	for i, msg := range got {
		assertMessage(t, msg, rows[i], "com.retailnetwork.logistics.node-topology."+wantTypes[i])
	}
	// Every outbox row is published, and the topic holds each exactly once.
	s.eventually(30*time.Second, "all outbox rows published", func() bool {
		var pending int
		_ = s.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&pending)
		return pending == 0
	})
	if n := kafkatest.Count(t, eventsTopic); int(n) != len(rows) {
		t.Fatalf("the topic holds %d messages, want exactly %d", n, len(rows))
	}
	assertPayloads(t, got, w)
}

func assertMessage(t *testing.T, msg kafkatest.Message, row outboxRow, wantType string) {
	t.Helper()
	e, err := cloudevents.Decode(msg.Value)
	if err != nil {
		t.Fatalf("%s: %v", wantType, err)
	}
	name := wantType[strings.LastIndex(wantType, ".")+1:]
	switch {
	case !bytes.Equal(msg.Value, row.value):
		t.Errorf("%s: the message is not the stored outbox bytes", wantType)
	case e.Type() != wantType || e.ID() != row.eventID || e.Subject() != row.subj || msg.Key != row.subj:
		t.Errorf("%s: type %q id %q subject %q key %q, want the outbox row %+v", wantType, e.Type(), e.ID(), e.Subject(), msg.Key, row)
	case e.Source() != "/retailnetwork/node-topology" || e.DataContentType() != "application/json" ||
		e.DataSchema() != "urn:retailnetwork:node-topology:events:"+name+":v1" || e.SpecVersion() != "1.0":
		t.Errorf("%s: attributes source %q dct %q dataschema %q spec %q", wantType, e.Source(), e.DataContentType(), e.DataSchema(), e.SpecVersion())
	case len(msg.Headers) != 1 || msg.Headers["content-type"] != cloudevents.MediaType:
		t.Errorf("%s: headers %v", wantType, msg.Headers)
	case e.Time().IsZero() || e.Time().Location() != time.UTC:
		t.Errorf("%s: time %v is not a UTC instant", wantType, e.Time())
	}
}

func dataOf(t *testing.T, msg kafkatest.Message) map[string]any {
	t.Helper()
	e, err := cloudevents.Decode(msg.Value)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data(), &data); err != nil {
		t.Fatalf("data of %s: %v", e.Type(), err)
	}
	return data
}

// assertPayloads checks the data of the events whose shape differs most.
func assertPayloads(t *testing.T, got []kafkatest.Message, w world) {
	t.Helper()
	if d := dataOf(t, got[0]); str(d, "regionId") != w.region || str(d, "code") != "SE" || str(d, "name") != "Southeast" {
		t.Errorf("RegionDefined data = %v", d)
	}
	fc, sc := dataOf(t, got[1]), dataOf(t, got[2])
	if str(fc, "nodeId") != w.fc || str(fc, "subtype") != "SORTABLE" || str(fc, "vendorId") != "vendor-1" || str(fc, "vendorNodeCode") != "FC-A" || str(fc, "operator") != "VENDOR" {
		t.Errorf("vendor FC NodeRegistered data = %v", fc)
	}
	for _, k := range []string{"subtype", "vendorId", "vendorNodeCode"} {
		if v, present := sc[k]; !present || v != nil {
			t.Errorf("network sort centre NodeRegistered: %s = %v (present %v), want an explicit null", k, v, present)
		}
	}
	prof := dataOf(t, got[5])
	if prof["sortDwellMinutes"] != float64(25) || prof["profileRevision"] != float64(1) || str(prof, "declaredBy") != "NETWORK_OPS" || prof["handlingTimeMinutes"] != float64(45) {
		t.Errorf("sort centre NodeOperationalProfileDeclared data = %v", prof)
	}
	if _, dwell := dataOf(t, got[4])["sortDwellMinutes"]; dwell {
		t.Error("the FC profile event carries a sortDwellMinutes")
	}
	if lane := dataOf(t, got[10]); str(lane, "laneId") != w.lane || str(lane, "originNodeId") != w.fc || str(lane, "mode") != "GROUND" || lane["nominalTransitMinutes"] != float64(120) {
		t.Errorf("LaneOpened data = %v", lane)
	}
	if area := dataOf(t, got[11]); str(area, "postalPrefix") != "01310" || str(area, "deliveryStationNodeId") != w.station || str(area, "regionId") != w.region || area["assignmentVersion"] != float64(1) {
		t.Errorf("ServiceAreaAssigned data = %v", area)
	}
}

// assertKafkaDown stops the broker: /readyz must answer 503
// dependency-unavailable (the metadata check dials a fresh connection, so a
// cached answer cannot hide the outage), /healthz stays 200, a write is still
// accepted (the outbox decouples it from Kafka) and the process shuts down
// cleanly.
func assertKafkaDown(t *testing.T, s *service) {
	t.Helper()
	kafkatest.Stop(t)

	var last response
	s.eventually(60*time.Second, "/readyz 503", func() bool {
		last = s.get("/readyz")
		return last.status == http.StatusServiceUnavailable
	})
	if !strings.HasPrefix(last.header.Get("Content-Type"), "application/problem+json") || !strings.Contains(string(last.body), `"type":"dependency-unavailable"`) ||
		!strings.Contains(string(last.body), "kafka") || strings.Contains(string(last.body), "postgres") {
		t.Fatalf("/readyz = %d %s, want 503 dependency-unavailable naming only kafka", last.status, last.body)
	}
	if h := s.get("/healthz"); h.status != http.StatusOK {
		t.Fatalf("/healthz = %d with Kafka down, want 200 (liveness)", h.status)
	}
	s.write(http.MethodPost, "/regions", `{"code":"SW","name":"Southwest"}`, http.StatusCreated)
	var pending int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&pending); err != nil || pending < 1 {
		t.Fatalf("a write with Kafka down left %d unpublished outbox rows (err %v), want it queued", pending, err)
	}

	s.cancel()
	if err := s.waitStopped(30 * time.Second); err != nil {
		t.Fatalf("run(ctx) returned %v on a cancellation, want a clean shutdown", err)
	}
	if r := s.get("/healthz"); r.status != -1 {
		t.Fatalf("the listener still answers after the shutdown: %d", r.status)
	}
}
