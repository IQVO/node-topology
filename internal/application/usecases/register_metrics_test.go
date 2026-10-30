package usecases_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
)

// recordingMetrics counts the ports.NodeMetrics calls of a use case.
type recordingMetrics struct {
	mu         sync.Mutex
	registered int
	rejected   []string
}

var _ ports.NodeMetrics = (*recordingMetrics)(nil)

func (m *recordingMetrics) NodeRegistered(context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registered++
}

func (m *recordingMetrics) NodeRejected(_ context.Context, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected = append(m.rejected, reason)
}

func withRegion(e *env) { e.seedRegion("r-1", "SP") }

func TestRegisterNodeCountsARegistration(t *testing.T) {
	e := newEnv(t)
	withRegion(e)
	m := &recordingMetrics{}
	e.deps.Metrics = m
	if _, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), fcCommand()); err != nil {
		t.Fatal(err)
	}
	if m.registered != 1 || len(m.rejected) != 0 {
		t.Fatalf("registered=%d rejected=%v, want 1 / none", m.registered, m.rejected)
	}
}

func TestRegisterNodeCountsEachRejectionOnceWithItsReason(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*env)
		mutate func(*usecases.RegisterNodeCommand)
		want   string
	}{
		{"unknown region", noSetup, func(*usecases.RegisterNodeCommand) {}, "unknown-region"},
		{"validation", withRegion, func(c *usecases.RegisterNodeCommand) { c.Timezone = "" }, "validation-failed"},
		{"operator", withRegion, func(c *usecases.RegisterNodeCommand) { c.Type = node.DeliveryStation; c.Vendor = nil }, "operator-not-allowed-for-node-type"},
		{"internal", func(e *env) { withRegion(e); e.nodes.saveErrs = []error{errBoom} }, func(*usecases.RegisterNodeCommand) {}, "internal-error"},
		{"retried conflict", func(e *env) {
			withRegion(e)
			e.nodes.saveErrs = []error{ports.ErrConcurrentUpdate, ports.ErrConcurrentUpdate}
		}, func(*usecases.RegisterNodeCommand) {}, "concurrent-update"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			c.setup(e)
			m := &recordingMetrics{}
			e.deps.Metrics = m
			cmd := fcCommand()
			c.mutate(&cmd)
			if _, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), cmd); err == nil {
				t.Fatal("the registration must be refused")
			}
			if m.registered != 0 || !reflect.DeepEqual(m.rejected, []string{c.want}) {
				t.Fatalf("registered=%d rejected=%v, want 0 / [%s]", m.registered, m.rejected, c.want)
			}
		})
	}
}

func TestRegisterNodeWithoutMetricsStillWorks(t *testing.T) {
	e := newEnv(t)
	withRegion(e)
	if _, err := usecases.NewRegisterNode(e.deps).Execute(context.Background(), fcCommand()); err != nil {
		t.Fatal(err)
	}
}
