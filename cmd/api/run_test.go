package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunRefusesAMissingDatabaseURLBeforeAnythingOpens(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("KAFKA_BROKERS", "k:9092")
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v, want a boot error naming DATABASE_URL", err)
	}
}

func TestRunRefusesMissingBrokers(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	t.Setenv("KAFKA_BROKERS", "")
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "KAFKA_BROKERS") {
		t.Fatalf("err = %v, want a boot error naming KAFKA_BROKERS", err)
	}
}

func TestServeUntilSignalExitsNonZeroWhenTheRelayEndsOnItsOwn(t *testing.T) {
	done := make(chan struct{})
	close(done)
	stopped := false
	err := serveUntilSignal(context.Background(), quiet(), &worker{name: "outbox-relay", done: done}, func() { stopped = true })
	if err == nil || !strings.Contains(err.Error(), "outbox-relay stopped unexpectedly") || !stopped {
		t.Fatalf("err = %v stopped=%v, want a non-nil error and the relay stopped", err, stopped)
	}
}

func TestServeUntilSignalStopsTheRelayOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &worker{name: "outbox-relay", done: make(chan struct{})}
	stop := func() { close(w.done) }
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if err := serveUntilSignal(ctx, quiet(), w, stop); err != nil {
		t.Fatalf("a clean shutdown returned %v", err)
	}
	select {
	case <-w.done:
	default:
		t.Fatal("the relay was not stopped")
	}
}
