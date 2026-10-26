package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	inboundhttp "github.com/claudioed/node-topology/internal/adapters/inbound/http"
	"github.com/claudioed/node-topology/internal/adapters/outbound/clock"
	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	outboxrelay "github.com/claudioed/node-topology/internal/adapters/outbound/outbox"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres/pgtx"
	"github.com/claudioed/node-topology/internal/adapters/outbound/telemetry"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/bootretry"
)

// run boots the service and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the integration test stops the real entry point that way).
func run(ctx context.Context) error {
	logger := newLogger(os.Getenv(envLogLevel))
	slog.SetDefault(logger)

	// Misconfiguration is refused before anything is opened or bound.
	cfg, err := loadConfig(os.Getenv, logger)
	if err != nil {
		return err
	}
	// Telemetry comes up before the router is built (otelchimetric binds the
	// global MeterProvider at construction) and is flushed LAST: deferred before
	// everything below, so the final spans of the drain still leave the process.
	tel := loadTelemetryEnv(os.Getenv)
	flushTelemetry, err := startTelemetry(logger, tel)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	// closeAdapters (pool.Close) is deferred FIRST so it runs LAST, after the
	// HTTP drain and the relay have stopped.
	ad, closeAdapters, err := buildAdapters(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer closeAdapters()
	// After Setup, so the counter binds to the real MeterProvider.
	nodeMetrics, err := telemetry.NewNodeMetrics()
	if err != nil {
		return err
	}
	ad.deps.Metrics = nodeMetrics

	readiness := newReadiness(ad.pool.Ping, cfg.brokers)
	handler, err := inboundhttp.NewRouter(buildServer(ad, readiness, logger, tel.serviceName))
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: cfg.addr, Handler: handler, ReadHeaderTimeout: defaultReadHeaderLimit}

	relay, stopRelay := startOutboxRelay(ad.outbox, cfg, logger)
	return serveUntilSignal(ctx, logger, httpServer, readiness, cfg.drainDelay, relay, stopRelay)
}

// newReadiness builds the /readyz gate: the drain gate plus a Postgres ping and
// a Kafka broker answering a topic-less metadata request (never topics: the
// broker auto-creates them on first write), cached 3 s, bounded 2 s. The ping
// is a parameter so the gate is testable without a pool.
func newReadiness(ping func(context.Context) error, brokers string) *inboundhttp.Readiness {
	return inboundhttp.NewReadiness(inboundhttp.DefaultReadinessTTL, inboundhttp.DefaultReadinessTimeout,
		inboundhttp.Dependency{Name: "postgres", Check: ping},
		inboundhttp.Dependency{Name: "kafka", Check: outboundkafka.NewBrokerCheck(brokers).Check},
	)
}

// adapters is the set of outbound adapters the composition root wires.
type adapters struct {
	pool   *pgxpool.Pool
	outbox *postgres.OutboxRepo
	deps   usecases.Deps
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
	return postgresAdapters(pool), pool.Close, nil
}

func postgresAdapters(pool *pgxpool.Pool) adapters {
	outbox := postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder())
	return adapters{pool: pool, outbox: outbox, deps: usecases.Deps{
		Nodes: postgres.NewNodeRepo(pool), Lanes: postgres.NewLaneRepo(pool),
		ServiceAreas: postgres.NewServiceAreaRepo(pool), Regions: postgres.NewRegionRepo(pool),
		Outbox: outbox, UoW: postgres.NewUnitOfWork(pool), Clock: clock.System{}, IDs: ids.UUID{},
	}}
}

// buildServer wires every use case into the REST server; serviceName names its
// server spans and HTTP metrics.
func buildServer(ad adapters, readiness *inboundhttp.Readiness, logger *slog.Logger, serviceName string) *inboundhttp.Server {
	d := ad.deps
	return &inboundhttp.Server{
		RegisterNode:       usecases.NewRegisterNode(d),
		DeclareProfile:     usecases.NewDeclareOperationalProfile(d),
		ActivateNode:       usecases.NewActivateNode(d),
		SuspendNode:        usecases.NewSuspendNode(d),
		RetireNode:         usecases.NewRetireNode(d),
		GetNode:            usecases.NewGetNode(d.Nodes),
		ListNodes:          usecases.NewListNodes(d.Nodes),
		OpenLane:           usecases.NewOpenLane(d),
		CloseLane:          usecases.NewCloseLane(d),
		ListLanes:          usecases.NewListLanes(d.Lanes),
		AssignServiceArea:  usecases.NewAssignServiceArea(d),
		ResolveServiceArea: usecases.NewResolveServiceArea(d.ServiceAreas),
		DefineRegion:       usecases.NewDefineRegion(d),
		GetRegion:          usecases.NewGetRegion(d.Regions),
		ListRegions:        usecases.NewListRegions(d.Regions),
		Idempotency:        inboundhttp.RequireIdempotencyKey(ad.pool, pgtx.With),
		Readiness:          readiness,
		Logger:             logger,
		ServiceName:        serviceName,
	}
}

// serveUntilSignal runs httpServer until SIGINT/SIGTERM, ctx cancellation or a
// listen error, then shuts down in the fleet order: (1) /readyz flips to 503,
// (2) wait the drain delay, (3) drain HTTP, (4) stop and await the relay LAST,
// so nothing committed above is stranded; the caller's deferred pool.Close runs
// after all of it. When the relay returns on its own the service cannot run:
// it exits non-zero so the orchestrator restarts it.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, httpServer *http.Server, readiness *inboundhttp.Readiness,
	drainDelay time.Duration, relay *worker, stopRelay func(),
) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		stopRelay()
		await(logger, relay)
		return fmt.Errorf("http server: %w", err)
	case <-relay.done:
		stopRelay()
		_ = httpServer.Close()
		return fmt.Errorf("%s stopped unexpectedly: exiting so the orchestrator restarts the service", relay.name)
	case <-sigCtx.Done():
	}

	readiness.SetNotReady()
	if drainDelay > 0 {
		logger.Info("shutdown: readiness flipped to not-ready; waiting for traffic to drain", "drain_delay", drainDelay.String())
		time.Sleep(drainDelay)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := httpServer.Shutdown(shutdownCtx)
	stopRelay()
	await(logger, relay)
	return err
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
