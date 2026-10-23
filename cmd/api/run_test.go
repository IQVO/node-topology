package main

import (
	"context"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/application/usecases"
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

func TestBuildServerWiresEveryRouteDependency(t *testing.T) {
	s := buildServer(adapters{deps: usecases.Deps{}}, newReadiness(func(context.Context) error { return nil }, "127.0.0.1:1"), quiet())
	v := reflect.ValueOf(*s)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsZero() {
			t.Errorf("Server.%s is not wired", v.Type().Field(i).Name)
		}
	}
	if _, err := inboundhttp.NewRouter(s); err != nil {
		t.Fatalf("router: %v", err)
	}
}

func TestReadinessNamesEachFailingDependency(t *testing.T) {
	g := newReadiness(func(context.Context) error { return net.ErrClosed }, "127.0.0.1:1")
	got := g.Failing(context.Background())
	if len(got) != 2 || got[0] != "postgres" || got[1] != "kafka" {
		t.Fatalf("failing = %v, want postgres and kafka", got)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

func newWorker() (*worker, func()) {
	w := &worker{name: "outbox-relay", done: make(chan struct{})}
	return w, func() {
		select {
		case <-w.done:
		default:
			close(w.done)
		}
	}
}

func TestServeUntilSignalExitsNonZeroWhenTheRelayEndsOnItsOwn(t *testing.T) {
	w, stop := newWorker()
	stop()
	srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	err := serveUntilSignal(context.Background(), quiet(), srv, inboundhttp.NewReadiness(0, 0), 0, w, stop)
	if err == nil || !strings.Contains(err.Error(), "outbox-relay stopped unexpectedly") {
		t.Fatalf("err = %v, want a non-nil error naming the relay", err)
	}
}

func TestServeUntilSignalShutsDownInOrderOnCancellation(t *testing.T) {
	w, stop := newWorker()
	gate := inboundhttp.NewReadiness(0, 0)
	srv := &http.Server{Addr: freeAddr(t), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	if err := serveUntilSignal(ctx, quiet(), srv, gate, 10*time.Millisecond, w, stop); err != nil {
		t.Fatalf("a clean shutdown returned %v", err)
	}
	if gate.Ready() {
		t.Fatal("readiness was not flipped before the listener closed")
	}
	select {
	case <-w.done:
	default:
		t.Fatal("the relay was not stopped")
	}
}

func TestServeUntilSignalReportsAListenError(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()
	w, stop := newWorker()
	srv := &http.Server{Addr: taken.Addr().String(), Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	err = serveUntilSignal(context.Background(), quiet(), srv, inboundhttp.NewReadiness(0, 0), 0, w, stop)
	if err == nil || !strings.Contains(err.Error(), "http server") {
		t.Fatalf("err = %v, want an http server error", err)
	}
}
