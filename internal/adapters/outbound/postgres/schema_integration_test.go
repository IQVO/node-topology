//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}

func TestIdempotencyKeysAreUniquePerRouteAndKey(t *testing.T) {
	pool := newPool(t)
	const insert = `INSERT INTO idempotency_keys (route, key, request_hash) VALUES ($1, $2, $3)`

	if err := exec(t, pool, insert, "POST /nodes", "k-1", "hash-a"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := exec(t, pool, insert, "POST /nodes", "k-1", "hash-b"); err == nil {
		t.Fatal("the same (route, key) was stored twice")
	}
	if err := exec(t, pool, insert, "POST /lanes", "k-1", "hash-a"); err != nil {
		t.Fatalf("the same key on another route must be accepted: %v", err)
	}
}

func TestIdempotencyKeysExpireAfterTwentyFourHours(t *testing.T) {
	pool := newPool(t)
	const insert = `INSERT INTO idempotency_keys (route, key, request_hash, response_status, response_body, created_at)
		VALUES ('POST /nodes', $1, 'h', 201, '{}', now() - $2::interval)`
	for key, age := range map[string]string{"old": "25 hours", "fresh": "23 hours", "now": "0 seconds"} {
		if err := exec(t, pool, insert, key, age); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec(t, pool, `DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'`); err != nil {
		t.Fatal(err)
	}
	if got := count(t, pool, "idempotency_keys"); got != 2 {
		t.Fatalf("rows after the TTL cleanup = %d, want 2 (old removed, fresh and now kept)", got)
	}
	var indexdef string
	err := pool.QueryRow(context.Background(), `SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_idempotency_keys_created_at'`).Scan(&indexdef)
	if err != nil {
		t.Fatalf("the TTL cleanup index is missing: %v", err)
	}
}

func TestOutboxEventsAreUniquePerEventIDAndTopic(t *testing.T) {
	pool := newPool(t)
	const insert = `INSERT INTO outbox_events (event_id, topic, event_type, subject, dataschema, value)
		VALUES ($1, $2, 'com.retailnetwork.t.node-topology.region.RegionDefined', 'r-1', 'urn:x', '{}')`
	if err := exec(t, pool, insert, "ev-1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := exec(t, pool, insert, "ev-1", "a"); err == nil {
		t.Fatal("the same (event_id, topic) was stored twice")
	}
	if err := exec(t, pool, insert, "ev-1", "b"); err != nil {
		t.Fatalf("the same event on another topic must be accepted: %v", err)
	}
	var attempts int
	var published *time.Time
	err := pool.QueryRow(context.Background(), `SELECT attempts, published_at FROM outbox_events LIMIT 1`).Scan(&attempts, &published)
	if err != nil || attempts != 0 || published != nil {
		t.Fatalf("a new row must be unpublished with 0 attempts: attempts=%d published=%v err=%v", attempts, published, err)
	}
}
