package postgres_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDatabaseURLIsMandatoryAndNamed(t *testing.T) {
	_, err := postgres.DatabaseURL(envOf(map[string]string{}))
	if !errors.Is(err, postgres.ErrMissingEnv) {
		t.Fatalf("error = %v, want ErrMissingEnv", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error %q does not name DATABASE_URL", err)
	}
	if _, err := postgres.DatabaseURL(envOf(map[string]string{"DATABASE_URL": ""})); err == nil {
		t.Fatal("an empty DATABASE_URL was accepted")
	}
	got, err := postgres.DatabaseURL(envOf(map[string]string{"DATABASE_URL": "postgres://pooled"}))
	if err != nil || got != "postgres://pooled" {
		t.Fatalf("url = %q, err = %v", got, err)
	}
}

func TestRequireEnvNamesTheVariable(t *testing.T) {
	_, err := postgres.RequireEnv(envOf(nil), "KAFKA_BROKERS")
	if !errors.Is(err, postgres.ErrMissingEnv) || !strings.Contains(err.Error(), "KAFKA_BROKERS") {
		t.Fatalf("error = %v, want ErrMissingEnv naming KAFKA_BROKERS", err)
	}
}

func TestMigrationsURLPrefersTheDirectDSNAndFallsBackToDatabaseURL(t *testing.T) {
	const pooled, direct = "postgres://pooled", "postgres://direct"
	if got := postgres.MigrationsURL(envOf(nil), pooled); got != pooled {
		t.Fatalf("fallback = %q, want %q", got, pooled)
	}
	if got := postgres.MigrationsURL(envOf(map[string]string{"MIGRATIONS_DATABASE_URL": ""}), pooled); got != pooled {
		t.Fatalf("empty override = %q, want %q", got, pooled)
	}
	if got := postgres.MigrationsURL(envOf(map[string]string{"MIGRATIONS_DATABASE_URL": direct}), pooled); got != direct {
		t.Fatalf("override = %q, want %q", got, direct)
	}
}

func TestNewPoolRejectsAnUnparsableURL(t *testing.T) {
	if _, err := postgres.NewPool(t.Context(), "://not a url"); err == nil {
		t.Fatal("an unparsable DSN produced a pool")
	}
}
