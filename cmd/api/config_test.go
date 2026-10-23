package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfigNamesTheMissingVariable(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr error
		wantMsg string
	}{
		{"no database url", map[string]string{"KAFKA_BROKERS": "k:9092"}, postgres.ErrMissingEnv, "DATABASE_URL"},
		{"no brokers", map[string]string{"DATABASE_URL": "postgres://x"}, ErrNoBrokers, "KAFKA_BROKERS"},
		{"blank brokers", map[string]string{"DATABASE_URL": "postgres://x", "KAFKA_BROKERS": " , ,"}, ErrNoBrokers, "KAFKA_BROKERS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfig(env(tc.env), quiet())
			if !errors.Is(err, tc.wantErr) || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want %v naming %s", err, tc.wantErr, tc.wantMsg)
			}
		})
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"DATABASE_URL": "postgres://app", "KAFKA_BROKERS": " a:9092 , b:9092,"}), quiet())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.databaseURL != "postgres://app" || cfg.migrationsURL != "postgres://app" || cfg.brokers != "a:9092,b:9092" ||
		cfg.addr != ":8080" || cfg.relayInterval != time.Second || cfg.maxAttempts != 20 || cfg.drainDelay != 5*time.Second {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadConfigReadsEveryOverride(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"DATABASE_URL": "postgres://pooled", "MIGRATIONS_DATABASE_URL": "postgres://direct", "KAFKA_BROKERS": "k:9092",
		"HTTP_ADDR": ":9090", "OUTBOX_RELAY_INTERVAL": "250ms", "OUTBOX_MAX_ATTEMPTS": "3", "SHUTDOWN_DRAIN_DELAY": "0", "LOG_LEVEL": "debug",
	}), quiet())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.migrationsURL != "postgres://direct" || cfg.databaseURL != "postgres://pooled" || cfg.addr != ":9090" ||
		cfg.relayInterval != 250*time.Millisecond || cfg.maxAttempts != 3 || cfg.drainDelay != 0 || cfg.logLevel != "debug" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestParseRelayInterval(t *testing.T) {
	cases := map[string]time.Duration{
		"": time.Second, "2s": 2 * time.Second, "1.5": 1500 * time.Millisecond, "250ms": 250 * time.Millisecond,
		"0": time.Second, "-1s": time.Second, "soon": time.Second,
	}
	for raw, want := range cases {
		if got := parseRelayInterval(raw, quiet()); got != want {
			t.Errorf("parseRelayInterval(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestOutboxMaxAttempts(t *testing.T) {
	cases := map[string]int{"": 20, "5": 5, "\t7": 7, "0": 0, "-1": 20, "many": 20}
	for raw, want := range cases {
		if got := outboxMaxAttempts(raw, quiet()); got != want {
			t.Errorf("outboxMaxAttempts(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestAnInvalidCapIsWarnedAboutNeverSilentlyUnlimited(t *testing.T) {
	var buf bytes.Buffer
	if got := outboxMaxAttempts("-4", slog.New(slog.NewTextHandler(&buf, nil))); got != 20 || !strings.Contains(buf.String(), "OUTBOX_MAX_ATTEMPTS") {
		t.Fatalf("got %d, log %q: want the default and a warning naming the variable", got, buf.String())
	}
}

func TestShutdownDrainDelay(t *testing.T) {
	cases := map[string]time.Duration{"": 5 * time.Second, "0": 0, "0s": 0, "30s": 30 * time.Second, "-1s": 5 * time.Second, "x": 5 * time.Second}
	for raw, want := range cases {
		if got := shutdownDrainDelay(raw, quiet()); got != want {
			t.Errorf("shutdownDrainDelay(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestNewLogLevel(t *testing.T) {
	cases := map[string]slog.Level{"debug": slog.LevelDebug, "DEBUG": slog.LevelDebug, "warn": slog.LevelWarn, "warning": slog.LevelWarn,
		"error": slog.LevelError, "info": slog.LevelInfo, "": slog.LevelInfo, "loud": slog.LevelInfo}
	for raw, want := range cases {
		if got := newLogLevel(raw); got != want {
			t.Errorf("newLogLevel(%q) = %v, want %v", raw, got, want)
		}
	}
}
