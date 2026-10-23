//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/application/outbox"
	"github.com/claudioed/node-topology/internal/domain/region"
)

var errSend = errors.New("broker said no")

// appendRegions stores one RegionDefined outbox row per code, in order.
func appendRegions(t *testing.T, pool *pgxpool.Pool, codes ...string) *postgres.OutboxRepo {
	t.Helper()
	repo := postgres.NewOutboxRepo(pool, testEncoder{})
	for i, code := range codes {
		_, events := definedEvents(t, region.ID(fmt.Sprintf("region-%d", i+1)), code)
		noErr(t, repo.Append(context.Background(), events...))
	}
	return repo
}

type sentLog struct {
	mu   sync.Mutex
	subj []string
}

func (s *sentLog) add(m outbox.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subj = append(s.subj, m.Subject)
}

type outboxRow struct {
	published bool
	attempts  int
	lastError *string
}

func rowOf(t *testing.T, pool *pgxpool.Pool, subject string) outboxRow {
	t.Helper()
	var r outboxRow
	err := pool.QueryRow(context.Background(),
		`SELECT published_at IS NOT NULL, attempts, last_error FROM outbox_events WHERE subject = $1`, subject).Scan(&r.published, &r.attempts, &r.lastError)
	noErr(t, err)
	return r
}

func TestDrainPublishesInIdOrderAndMarksRows(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA", "BB", "CC")
	var sent sentLog

	n, err := repo.Drain(context.Background(), 10, 0, func(_ context.Context, m outbox.Message) error { sent.add(m); return nil })

	noErr(t, err)
	if n != 3 || fmt.Sprint(sent.subj) != "[region-1 region-2 region-3]" {
		t.Fatalf("published %d in order %v, want 3 in id order", n, sent.subj)
	}
	for _, s := range sent.subj {
		if r := rowOf(t, pool, s); !r.published || r.attempts != 1 || r.lastError != nil {
			t.Fatalf("%s row = %+v, want published after one attempt", s, r)
		}
	}
	n, err = repo.Drain(context.Background(), 10, 0, func(context.Context, outbox.Message) error { return errSend })
	if noErr(t, err); n != 0 {
		t.Fatalf("a drained outbox published %d more rows", n)
	}
}

func TestDrainHonoursTheBatchLimit(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA", "BB", "CC")
	n, err := repo.Drain(context.Background(), 2, 0, func(context.Context, outbox.Message) error { return nil })
	noErr(t, err)
	if n != 2 || rowOf(t, pool, "region-3").published {
		t.Fatalf("published %d rows, third published=%v, want 2 and the third left", n, rowOf(t, pool, "region-3").published)
	}
}

func TestDrainStopsAtTheFirstFailureAndRecordsIt(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA", "BB", "CC")
	var sent sentLog

	n, err := repo.Drain(context.Background(), 10, 0, func(_ context.Context, m outbox.Message) error {
		if m.Subject == "region-2" {
			return errSend
		}
		sent.add(m)
		return nil
	})

	var sf *outbox.SendFailure
	if !errors.As(err, &sf) || !errors.Is(err, errSend) || n != 1 {
		t.Fatalf("n=%d err=%v, want one row published then a SendFailure wrapping the send error", n, err)
	}
	if sf.Attempts != 1 || sf.Message.Subject != "region-2" || sf.Message.EventType == "" || string(sf.Message.Key) != "region-2" {
		t.Fatalf("failure = %+v", sf)
	}
	first, second, third := rowOf(t, pool, "region-1"), rowOf(t, pool, "region-2"), rowOf(t, pool, "region-3")
	if !first.published || second.published || third.published || third.attempts != 0 {
		t.Fatalf("rows = %+v %+v %+v: region-1 must be committed, region-2 failed, region-3 untouched", first, second, third)
	}
	if second.attempts != 1 || second.lastError == nil || *second.lastError != errSend.Error() {
		t.Fatalf("failed row = %+v, want one attempt and the error text", second)
	}
}

// TestDrainSkipsAPoisonRowOnceItReachedTheCap is the attempt-cap behaviour:
// the poison row stays (attempts, last_error) and the good row behind it
// publishes; re-arming the poison row (attempts = 0) lets it go again.
func TestDrainSkipsAPoisonRowOnceItReachedTheCap(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA", "BB")
	poison := func(_ context.Context, m outbox.Message) error {
		if m.Subject == "region-1" {
			return errSend
		}
		return nil
	}
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := repo.Drain(context.Background(), 10, 2, poison)
		var sf *outbox.SendFailure
		if !errors.As(err, &sf) || sf.Attempts != attempt {
			t.Fatalf("pass %d: err = %v, want a SendFailure at attempt %d", attempt, err, attempt)
		}
	}

	n, err := repo.Drain(context.Background(), 10, 2, poison)

	noErr(t, err)
	poisoned, good := rowOf(t, pool, "region-1"), rowOf(t, pool, "region-2")
	if n != 1 || poisoned.published || poisoned.attempts != 2 || poisoned.lastError == nil || !good.published {
		t.Fatalf("n=%d poison=%+v good=%+v, want the capped poison row kept and the good row published", n, poisoned, good)
	}

	_, err = pool.Exec(context.Background(), `UPDATE outbox_events SET attempts = 0 WHERE subject = 'region-1'`)
	noErr(t, err)
	n, err = repo.Drain(context.Background(), 10, 2, func(context.Context, outbox.Message) error { return nil })
	if noErr(t, err); n != 1 || !rowOf(t, pool, "region-1").published {
		t.Fatalf("after re-arming n=%d, want the poison row published", n)
	}
}

func TestDrainWithoutACapKeepsRetryingAFailingRow(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA")
	for range 4 {
		_, err := repo.Drain(context.Background(), 10, 0, func(context.Context, outbox.Message) error { return errSend })
		if err == nil {
			t.Fatal("want the send failure")
		}
	}
	if r := rowOf(t, pool, "region-1"); r.attempts != 4 || r.published {
		t.Fatalf("row = %+v, want four attempts and still unpublished", r)
	}
}

// TestConcurrentDrainsNeverSendARowTwice: FOR UPDATE SKIP LOCKED.
func TestConcurrentDrainsNeverSendARowTwice(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "A1", "A2", "A3", "A4", "A5", "A6", "A7", "A8")
	var (
		sent sentLog
		wg   sync.WaitGroup
	)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Drain(context.Background(), 3, 0, func(_ context.Context, m outbox.Message) error { sent.add(m); return nil })
			if err != nil {
				t.Errorf("drain: %v", err)
			}
		}()
	}
	wg.Wait()

	seen := map[string]int{}
	for _, s := range sent.subj {
		seen[s]++
	}
	for s, n := range seen {
		if n != 1 {
			t.Errorf("%s was sent %d times", s, n)
		}
	}
}

func TestDrainReportsAMalformedHeadersRow(t *testing.T) {
	pool := newPool(t)
	repo := appendRegions(t, pool, "AA")
	_, err := pool.Exec(context.Background(), `UPDATE outbox_events SET headers = '{"not":"a list"}'`)
	noErr(t, err)
	if _, err := repo.Drain(context.Background(), 1, 0, func(context.Context, outbox.Message) error { return nil }); err == nil {
		t.Fatal("a row with undecodable headers must fail the pass")
	}
}
