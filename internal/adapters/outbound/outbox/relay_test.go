package outbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/application/outbox"
)

// fakeStore is an in-memory outbox with Drain semantics: id order, stop at the
// first send failure, attempts counted, rows at the cap not claimed.
type fakeStore struct {
	mu   sync.Mutex
	rows []*fakeRow
	// limits and caps seen by Drain
	limits, caps []int
}

type fakeRow struct {
	msg       outbox.Message
	published bool
	attempts  int
}

func newStore(types ...string) *fakeStore {
	s := &fakeStore{}
	for _, ty := range types {
		s.rows = append(s.rows, &fakeRow{msg: outbox.Message{EventType: ty, Topic: "t", Key: []byte("k-" + ty)}})
	}
	return s
}

func (s *fakeStore) Drain(ctx context.Context, limit, maxAttempts int, send func(context.Context, outbox.Message) error) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = append(s.limits, limit)
	s.caps = append(s.caps, maxAttempts)
	n := 0
	for i, r := range s.rows {
		if n >= limit {
			break
		}
		if r.published || (maxAttempts > 0 && r.attempts >= maxAttempts) {
			continue
		}
		if err := send(ctx, r.msg); err != nil {
			r.attempts++
			return n, &outbox.SendFailure{RowID: int64(i + 1), Message: r.msg, Attempts: r.attempts, Err: err}
		}
		r.published = true
		r.attempts++
		n++
	}
	return n, nil
}

type fakeSink struct {
	mu     sync.Mutex
	sent   []string
	failOn map[string]error
}

func (s *fakeSink) Send(_ context.Context, msgs ...outbox.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range msgs {
		if err := s.failOn[m.EventType]; err != nil {
			return err
		}
		s.sent = append(s.sent, m.EventType)
	}
	return nil
}

func (s *fakeSink) types() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRelayOncePublishesInOrderAndMarksRows(t *testing.T) {
	store, sink := newStore("a", "b", "c"), &fakeSink{}
	r := NewOutboxRelay(store, sink, WithLogger(quiet()), WithBatchSize(2), WithMaxAttempts(5))
	n, err := r.RelayOnce(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("first pass: n=%d err=%v, want 2 rows", n, err)
	}
	n, err = r.RelayOnce(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("second pass: n=%d err=%v, want 1 row", n, err)
	}
	if got := strings.Join(sink.types(), ","); got != "a,b,c" {
		t.Fatalf("sent %s, want a,b,c in order", got)
	}
	if store.limits[0] != 2 || store.caps[0] != 5 {
		t.Fatalf("Drain called with limit %d cap %d, want 2 and 5", store.limits[0], store.caps[0])
	}
}

func TestRelayOnceStopsAtTheFirstFailureAndKeepsTheRow(t *testing.T) {
	store := newStore("a", "b", "c")
	sink := &fakeSink{failOn: map[string]error{"b": errors.New("broker down")}}
	r := NewOutboxRelay(store, sink, WithLogger(quiet()))
	n, err := r.RelayOnce(context.Background())
	var sf *outbox.SendFailure
	if !errors.As(err, &sf) || n != 1 || sf.Attempts != 1 || sf.Message.EventType != "b" {
		t.Fatalf("n=%d err=%v, want 1 row published then a SendFailure on b", n, err)
	}
	if store.rows[1].published || store.rows[2].published {
		t.Fatal("b failed, so neither b nor c may be published (c must not overtake b)")
	}
}

func TestDefaultsAreTheDocumentedOnes(t *testing.T) {
	r := NewOutboxRelay(newStore(), &fakeSink{})
	if r.interval != time.Second || r.batchSize != 100 || r.maxAttempts != 0 || r.logger == nil {
		t.Fatalf("defaults = interval %v batch %d cap %d logger %v", r.interval, r.batchSize, r.maxAttempts, r.logger)
	}
	if DefaultMaxAttempts != 20 {
		t.Fatalf("DefaultMaxAttempts = %d, want 20", DefaultMaxAttempts)
	}
}

func TestOptionsIgnoreInvalidValues(t *testing.T) {
	r := NewOutboxRelay(newStore(), &fakeSink{}, WithInterval(0), WithInterval(-time.Second), WithBatchSize(0), WithBatchSize(-1),
		WithMaxAttempts(-3), WithLogger(nil))
	if r.interval != DefaultInterval || r.batchSize != DefaultBatchSize || r.maxAttempts != 0 || r.logger == nil {
		t.Fatalf("invalid options must be ignored, got interval %v batch %d cap %d", r.interval, r.batchSize, r.maxAttempts)
	}
	r = NewOutboxRelay(newStore(), &fakeSink{}, WithInterval(time.Minute), WithBatchSize(7), WithMaxAttempts(0))
	if r.interval != time.Minute || r.batchSize != 7 || r.maxAttempts != 0 {
		t.Fatalf("valid options must apply, got interval %v batch %d cap %d", r.interval, r.batchSize, r.maxAttempts)
	}
}

func capLogs(t *testing.T, maxAttempts int, failures int) (string, *fakeStore) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	store := newStore("poison", "good")
	sink := &fakeSink{failOn: map[string]error{"poison": errors.New("record too large")}}
	r := NewOutboxRelay(store, sink, WithLogger(logger), WithMaxAttempts(maxAttempts))
	for range failures {
		_, _ = r.RelayOnce(context.Background())
	}
	return buf.String(), store
}

func TestTheFailureThatReachesTheCapIsLoggedAtErrorOnce(t *testing.T) {
	out, store := capLogs(t, 3, 3)
	if strings.Count(out, "reached the attempt cap") != 1 {
		t.Fatalf("log = %q, want exactly one cap message", out)
	}
	for _, want := range []string{"level=ERROR", "outbox_id=1", "topic=t", "event_type=poison", "key=k-poison", "attempts=3", "max_attempts=3", "record too large"} {
		if !strings.Contains(out, want) {
			t.Errorf("cap log lacks %q: %s", want, out)
		}
	}
	if store.rows[0].attempts != 3 {
		t.Fatalf("poison attempts = %d, want 3", store.rows[0].attempts)
	}
}

func TestNoCapMessageBeforeTheCapOrWhenUnlimited(t *testing.T) {
	if out, _ := capLogs(t, 3, 2); strings.Contains(out, "attempt cap") {
		t.Fatalf("two failures under a cap of 3 must not log the cap: %s", out)
	}
	if out, _ := capLogs(t, 0, 5); strings.Contains(out, "attempt cap") {
		t.Fatalf("an unlimited relay must never log the cap: %s", out)
	}
}

// TestACappedPoisonRowIsOvertakenByTheNextRow is the attempt-cap behaviour:
// after the cap the poison row is skipped and the good row publishes.
func TestACappedPoisonRowIsOvertakenByTheNextRow(t *testing.T) {
	var buf bytes.Buffer
	store := newStore("poison", "good")
	sink := &fakeSink{failOn: map[string]error{"poison": errors.New("record too large")}}
	r := NewOutboxRelay(store, sink, WithLogger(slog.New(slog.NewTextHandler(&buf, nil))), WithMaxAttempts(2))
	for range 2 {
		if n, err := r.RelayOnce(context.Background()); n != 0 || err == nil {
			t.Fatalf("n=%d err=%v, want the poison failure with nothing published", n, err)
		}
	}
	n, err := r.RelayOnce(context.Background())
	if err != nil || n != 1 || !store.rows[1].published || store.rows[0].published {
		t.Fatalf("n=%d err=%v good=%v poison=%v, want good published and poison left behind", n, err, store.rows[1].published, store.rows[0].published)
	}
}

func TestRunDrainsUntilTheContextIsCancelled(t *testing.T) {
	store, sink := newStore("a", "b", "c"), &fakeSink{}
	r := NewOutboxRelay(store, sink, WithLogger(quiet()), WithBatchSize(2), WithInterval(5*time.Millisecond), WithMaxAttempts(3))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(sink.types()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if got := strings.Join(sink.types(), ","); got != "a,b,c" {
		t.Fatalf("sent %s, want a,b,c", got)
	}
}

func TestRunRetriesAFailingPassAfterTheInterval(t *testing.T) {
	store := newStore("a")
	boom := errors.New("not yet")
	sink := &fakeSink{failOn: map[string]error{"a": boom}}
	r := NewOutboxRelay(store, sink, WithLogger(quiet()), WithInterval(5*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		failed := store.rows[0].attempts >= 2
		store.mu.Unlock()
		if failed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	sink.mu.Lock()
	delete(sink.failOn, "a")
	sink.mu.Unlock()
	for len(sink.types()) < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if got := sink.types(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("sent %v, want a once the sink recovers", got)
	}
}

func TestRunReturnsAtOnceWhenTheContextIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newStore("a")
	if err := NewOutboxRelay(store, &fakeSink{}, WithLogger(quiet())).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(store.limits) != 0 {
		t.Fatal("a cancelled relay must not drain")
	}
}

func TestSendFailureErrorAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	sf := &outbox.SendFailure{RowID: 7, Message: outbox.Message{EventType: "x.Y"}, Err: cause}
	if sf.Error() != "outbox row 7 (x.Y): boom" || !errors.Is(sf, cause) {
		t.Fatalf("error = %q", sf.Error())
	}
}
