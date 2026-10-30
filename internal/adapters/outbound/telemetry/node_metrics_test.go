package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/claudioed/node-topology/internal/adapters/outbound/telemetry"
)

func TestNodeMetricsCountsByOutcomeAndReason(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	metrics, err := telemetry.NewNodeMetrics()
	if err != nil {
		t.Fatalf("NewNodeMetrics: %v", err)
	}
	ctx := context.Background()
	metrics.NodeRegistered(ctx)
	metrics.NodeRegistered(ctx)
	metrics.NodeRejected(ctx, "vendor-node-code-taken")
	metrics.NodeRejected(ctx, "validation-failed")
	metrics.NodeRejected(ctx, "validation-failed")

	got := collectNodePoints(t, reader)
	want := map[string]int64{"registered/": 2, "rejected/vendor-node-code-taken": 1, "rejected/validation-failed": 2}
	if len(got) != len(want) {
		t.Fatalf("data points = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
}

// collectNodePoints reads the topology.nodes.registered data points as
// "outcome/reason" -> value, checking the instrument's unit and description.
func collectNodePoints(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "topology.nodes.registered" {
				addPoints(t, m, got)
			}
		}
	}
	return got
}

func addPoints(t *testing.T, m metricdata.Metrics, got map[string]int64) {
	t.Helper()
	if m.Unit != "{node}" || m.Description == "" {
		t.Errorf("unit = %q, description empty = %v; the convention wants a UCUM unit and a description", m.Unit, m.Description == "")
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("topology.nodes.registered is a %T, want an int64 Sum", m.Data)
	}
	for _, point := range sum.DataPoints {
		outcome, _ := point.Attributes.Value("outcome")
		reason, _ := point.Attributes.Value("reason")
		got[outcome.AsString()+"/"+reason.AsString()] = point.Value
	}
}

// Whatever the global provider is, recording is fire-and-forget.
func TestNodeMetricsRecordingNeverPanics(t *testing.T) {
	metrics, err := telemetry.NewNodeMetrics()
	if err != nil {
		t.Fatal(err)
	}
	metrics.NodeRegistered(context.Background())
	metrics.NodeRejected(context.Background(), "internal-error")
}
