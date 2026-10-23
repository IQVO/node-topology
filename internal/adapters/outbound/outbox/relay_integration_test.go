//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/kafka/cloudevents"
	"github.com/claudioed/node-topology/internal/adapters/outbound/clock"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	relay "github.com/claudioed/node-topology/internal/adapters/outbound/outbox"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/adapters/outbound/repocontract"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/testing/kafkatest"
	"github.com/claudioed/node-topology/internal/testing/pgtest"
)

// The relay is proven end to end against real infrastructure, both started
// with testcontainers (never skip-gated, never a hardcoded broker address):
// the real use cases write aggregates + outbox rows in real Postgres, the
// relay drains them to a real broker and the CloudEvents are read back from
// retailnetwork.node-topology.events.
//
// The topic does not exist before the first write: the broker auto-creates it
// and the FIRST write to it can fail with UnknownTopicOrPartition or
// LeaderNotAvailable. That is not an error of the relay: the failed row keeps
// its place (attempts + last_error), the pass is retried after the interval
// and the same CloudEvents id is published, so the test waits for the
// events instead of expecting the first pass to succeed.

const eventsTopic = "retailnetwork.node-topology.events"

type bench struct {
	pool *pgtest.Pool
	deps usecases.Deps
}

func newBench(t *testing.T) bench {
	t.Helper()
	pool := pgtest.NewPool(t)
	return bench{pool: pool, deps: usecases.Deps{
		Nodes: postgres.NewNodeRepo(pool), Lanes: postgres.NewLaneRepo(pool), ServiceAreas: postgres.NewServiceAreaRepo(pool),
		Regions: postgres.NewRegionRepo(pool), Outbox: postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder()),
		UoW: postgres.NewUnitOfWork(pool), Clock: clock.System{}, IDs: ids.UUID{},
	}}
}

// run writes the four events: RegionDefined, NodeRegistered,
// NodeOperationalProfileDeclared, NodeActivated, and returns the node id.
func (b bench) run(t *testing.T) node.ID {
	t.Helper()
	ctx := context.Background()
	reg, err := usecases.NewDefineRegion(b.deps).Execute(ctx, usecases.DefineRegionCommand{Code: "SE", Name: "Southeast"})
	if err != nil {
		t.Fatalf("define region: %v", err)
	}
	n, err := usecases.NewRegisterNode(b.deps).Execute(ctx, usecases.RegisterNodeCommand{
		Type: node.FulfillmentCenter, Subtype: node.SubtypeSortable, Operator: node.OperatorVendor,
		Vendor: &node.VendorRef{VendorID: "vendor-1", VendorNodeCode: "FC-1"}, RegionID: region.ID(reg.RegionID),
		Timezone: "America/Sao_Paulo", Location: node.GeoLocation{PostalCode: "01310-100", CountryCode: "BR"},
	})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	id := node.ID(n.NodeID)
	if _, err := usecases.NewDeclareOperationalProfile(b.deps).Execute(ctx, usecases.DeclareOperationalProfileCommand{NodeID: id, Declaration: repocontract.Declaration(1)}); err != nil {
		t.Fatalf("declare profile: %v", err)
	}
	if _, err := usecases.NewActivateNode(b.deps).Execute(ctx, id); err != nil {
		t.Fatalf("activate: %v", err)
	}
	return id
}

func startRelay(t *testing.T, b bench) {
	t.Helper()
	sink := outboundkafka.NewRelaySink(kafkatest.BrokerList(t))
	t.Cleanup(func() { _ = sink.Close() })
	r := relay.NewOutboxRelay(postgres.NewOutboxRepo(b.pool, nil), sink,
		relay.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), relay.WithInterval(100*time.Millisecond), relay.WithMaxAttempts(relay.DefaultMaxAttempts))
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()
	t.Cleanup(func() { stop(); <-done })
}

func TestRelayPublishesTheOutboxRowsAsExactlyTheCloudEvents(t *testing.T) {
	b := newBench(t)
	nodeID := b.run(t)
	var rows []struct{ eventID, eventType, subject string }
	dbRows, err := b.pool.Query(context.Background(), `SELECT event_id, event_type, subject FROM outbox_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for dbRows.Next() {
		var r struct{ eventID, eventType, subject string }
		if err := dbRows.Scan(&r.eventID, &r.eventType, &r.subject); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	dbRows.Close()
	if len(rows) != 4 {
		t.Fatalf("outbox holds %d rows, want 4", len(rows))
	}

	startRelay(t, b)
	got := kafkatest.ReadN(t, eventsTopic, 4, 90*time.Second)

	wantTypes := []string{"region.RegionDefined", "node.NodeRegistered", "node.NodeOperationalProfileDeclared", "node.NodeActivated"}
	for i, msg := range got {
		e, err := cloudevents.Decode(msg.Value)
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		wantType := "com.retailnetwork.logistics.node-topology." + wantTypes[i]
		if e.Type() != wantType || e.ID() != rows[i].eventID || e.Subject() != rows[i].subject || msg.Key != rows[i].subject {
			t.Fatalf("message %d = type %q id %q subject %q key %q, want the outbox row %+v of type %s", i, e.Type(), e.ID(), e.Subject(), msg.Key, rows[i], wantType)
		}
		name := strings.Split(wantTypes[i], ".")[1]
		if e.Source() != "/retailnetwork/node-topology" || e.DataSchema() != "urn:retailnetwork:node-topology:events:"+name+":v1" {
			t.Fatalf("message %d source/dataschema = %q / %q", i, e.Source(), e.DataSchema())
		}
		if len(msg.Headers) != 1 || msg.Headers["content-type"] != cloudevents.MediaType {
			t.Fatalf("message %d headers = %+v", i, msg.Headers)
		}
	}
	assertActivated(t, got[3].Value, nodeID)
	waitAllPublished(t, b.pool)
	if n := kafkatest.Count(t, eventsTopic); n != 4 {
		t.Fatalf("the topic holds %d messages, want exactly 4 (no duplicate publication)", n)
	}
}

func assertActivated(t *testing.T, value []byte, id node.ID) {
	t.Helper()
	e, err := cloudevents.Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		NodeID      string `json:"nodeId"`
		ActivatedAt string `json:"activatedAt"`
	}
	if err := json.Unmarshal(e.Data(), &data); err != nil || data.NodeID != string(id) || data.ActivatedAt == "" {
		t.Fatalf("NodeActivated data = %s (err %v), want nodeId %s and activatedAt", e.Data(), err, id)
	}
}

func waitAllPublished(t *testing.T, pool *pgtest.Pool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var pending int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_events WHERE published_at IS NULL`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d outbox rows still unpublished", pending)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
