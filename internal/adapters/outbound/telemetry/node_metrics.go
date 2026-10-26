package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/claudioed/node-topology/internal/application/ports"
)

// meterName scopes this service's own instruments, keeping them distinct from
// the ones otelchi and the runtime collector register.
const meterName = "github.com/claudioed/node-topology"

// nodesRegisteredName is the Tier 2 business metric of the context (ADR 0010,
// the fleet's standard metrics convention): every node registration, by outcome.
const nodesRegisteredName = "topology.nodes.registered"

const (
	outcomeKey = attribute.Key("outcome")
	reasonKey  = attribute.Key("reason")

	outcomeRegistered = "registered"
	outcomeRejected   = "rejected"
)

// NodeMetrics implements ports.NodeMetrics against the global MeterProvider.
// Until Setup installs a real provider the global one is a no-op, so recording
// is cheap and safe in tests and local runs.
type NodeMetrics struct {
	counter metric.Int64Counter
}

var _ ports.NodeMetrics = (*NodeMetrics)(nil)

// NewNodeMetrics registers the counter. It only fails if the instrument name
// is invalid, which is a programming error, not a runtime condition. Call it
// after Setup so the instrument binds to the real MeterProvider.
func NewNodeMetrics() (*NodeMetrics, error) {
	counter, err := otel.Meter(meterName).Int64Counter(
		nodesRegisteredName,
		metric.WithDescription("Node registrations by outcome (registered or rejected; reason says why a rejection happened, "+
			"as a problem slug such as vendor-node-code-taken or validation-failed). "+
			"A rising vendor-node-code-taken or operator-not-allowed-for-node-type means onboarding clients send registrations "+
			"the network rules refuse, not that the service is failing; a rising internal-error or concurrent-update means the service itself is."),
		metric.WithUnit("{node}"),
	)
	if err != nil {
		return nil, err
	}
	return &NodeMetrics{counter: counter}, nil
}

// NodeRegistered counts a node that was registered.
func (m *NodeMetrics) NodeRegistered(ctx context.Context) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeRegistered)))
}

// NodeRejected counts a registration that stored no node, with the reason why.
func (m *NodeMetrics) NodeRejected(ctx context.Context, reason string) {
	m.counter.Add(ctx, 1, metric.WithAttributes(outcomeKey.String(outcomeRejected), reasonKey.String(reason)))
}
