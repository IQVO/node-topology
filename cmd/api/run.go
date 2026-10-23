package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	outboxrelay "github.com/claudioed/node-topology/internal/adapters/outbound/outbox"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/bootretry"
)

// run boots the service and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the integration test stops the real entry point that way).
func run(ctx context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: newLogLevel(os.Getenv(envLogLevel))}))
	slog.SetDefault(logger)

	// Misconfiguration is refused before anything is opened or bound.
	cfg, err := loadConfig(os.Getenv, logger)
	if err != nil {
		return err
	}
	// closeAdapters (pool.Close) is deferred FIRST so it runs LAST, after the
	// relay has stopped.
	ad, closeAdapters, err := buildAdapters(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()

	relay, stopRelay := startOutboxRelay(ad.outbox, cfg, logger)
	return serveUntilSignal(ctx, logger, relay, stopRelay)
}

// adapters is the set of outbound adapters the composition root wires.
type adapters struct {
	pool   *pgxpool.Pool
	outbox *postgres.OutboxRepo
}

// buildAdapters runs the embedded migrations and wires the Postgres adapters.
// The first outbound dial of an injected pod can be reset, so the migrations
// and the first ping retry with backoff; on exhaustion the LAST error is
// returned and the process refuses to boot.
func buildAdapters(ctx context.Context, cfg config, logger *slog.Logger) (adapters, func(), error) {
	noop := func() {}
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error {
		return postgres.RunMigrations(cfg.migrationsURL)
	}); err != nil {
		return adapters{}, noop, err
	}
	pool, err := postgres.NewPool(ctx, cfg.databaseURL)
	if err != nil {
		return adapters{}, noop, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return adapters{}, noop, err
	}
	logger.Info("postgres adapters configured")
	return adapters{pool: pool, outbox: postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder())}, pool.Close, nil
}

// serveUntilSignal runs until SIGINT/SIGTERM or ctx is cancelled, then stops
// the relay and waits for it. When the relay returns on its own the service
// cannot run: it exits non-zero so the orchestrator restarts it.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, relay *worker, stopRelay func()) error {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-relay.done:
		stopRelay()
		return fmt.Errorf("%s stopped unexpectedly: exiting so the orchestrator restarts the service", relay.name)
	case <-sigCtx.Done():
	}
	stopRelay()
	await(logger, relay)
	return nil
}

// await waits (bounded) for w to return.
func await(logger *slog.Logger, w *worker) {
	select {
	case <-w.done:
	case <-time.After(workerDrainTimeout):
		logger.Warn("worker did not stop before the shutdown drain deadline", "worker", w.name)
	}
}

var _ outboxrelay.Store = (*postgres.OutboxRepo)(nil)
