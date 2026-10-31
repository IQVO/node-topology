//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/domain/region"
)

// An Append made under a span stores that span's traceparent in the row's
// headers, next to the encoder's own header, and a span-less Append stores none.
func TestOutboxAppendStoresTheActiveTraceparent(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })

	pool := newPool(t)
	repo := postgres.NewOutboxRepo(pool, testEncoder{})
	_, first := definedEvents(t, region.ID("region-1"), "AA")
	_, second := definedEvents(t, region.ID("region-2"), "BB")

	ctx, span := sdktrace.NewTracerProvider().Tracer("test").Start(context.Background(), "POST /regions")
	defer span.End()
	noErr(t, repo.Append(ctx, first...))
	noErr(t, repo.Append(context.Background(), second...))

	rows, err := pool.Query(context.Background(), `SELECT headers::text FROM outbox_events ORDER BY id`)
	noErr(t, err)
	defer rows.Close()
	var stored [][]struct{ Key, Value string }
	for rows.Next() {
		var raw string
		noErr(t, rows.Scan(&raw))
		var hs []struct{ Key, Value string }
		noErr(t, json.Unmarshal([]byte(raw), &hs))
		stored = append(stored, hs)
	}
	if len(stored) != 2 {
		t.Fatalf("stored %d rows, want 2", len(stored))
	}
	want := "00-" + span.SpanContext().TraceID().String() + "-" + span.SpanContext().SpanID().String() + "-01"
	if len(stored[0]) != 2 || stored[0][0].Key != "content-type" || stored[0][1].Key != "traceparent" || stored[0][1].Value != want {
		t.Errorf("row under a span has headers %v, want content-type and traceparent %s", stored[0], want)
	}
	if len(stored[1]) != 1 || stored[1][0].Key != "content-type" {
		t.Errorf("row without a span has headers %v, want only content-type", stored[1])
	}
}
