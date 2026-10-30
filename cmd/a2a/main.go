// Command a2a is the composition root for the node-topology A2A server
// (docs/adr/0011-a2a-inbound-adapter.md): it wires env config to the outbound
// Postgres repositories, the repositories to the SAME read use cases the REST
// and MCP adapters expose (GetNode, ListNodes, ResolveServiceArea), and those
// to the inbound A2A adapter, then serves Agent2Agent JSON-RPC over HTTP (the
// Agent Card at /.well-known/agent-card.json, JSON-RPC at / and /a2a). It is a
// third, independent deployable alongside cmd/api and cmd/mcp.
//
// The surface is READ-ONLY, exactly like cmd/mcp (ADR 0008): no write use case
// is wired here at all, so this binary can never append to the outbox. It
// never starts the outbox relay and never dials Kafka (no KAFKA_BROKERS, no
// consumer groups; this service consumes no topic anyway): the relay runs in
// cmd/api only. It makes no REST, MCP or A2A call to any sibling context (no
// outbound A2A client exists in this repository).
//
// There is NO auth of any kind (fleet-wide revert 2026-09-11): no keys, no
// bearer checks. Access control is the in-cluster ClusterIP boundary.
//
// Environment:
//
//	A2A_ADDR                 listen address (default :8093)
//	A2A_AGENT_URL            REQUIRED. Absolute http(s) URL clients dial, advertised on the Agent Card
//	DATABASE_URL             REQUIRED. Postgres DSN (the service database cmd/api owns)
//	MIGRATIONS_DATABASE_URL  direct DSN for the boot migrations (default DATABASE_URL)
//	LOG_LEVEL                debug | info | warn | error (default info)
//	OTEL_SERVICE_NAME        service.name of spans and metrics (default node-topology-a2a)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint (default telemetry.DefaultEndpoint); export never blocks
//	SERVICE_VERSION          service.version and the card version, logged at boot too (default dev)
//	ENVIRONMENT              deployment.environment.name, logged at boot too (default dev in the boot line, local in telemetry)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inbounda2a "github.com/claudioed/node-topology/internal/adapters/inbound/a2a"
	"github.com/claudioed/node-topology/internal/adapters/outbound/postgres"
	"github.com/claudioed/node-topology/internal/adapters/outbound/telemetry"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

// errNoDatabase is the boot error of a missing DATABASE_URL: this binary has
// no in-memory mode.
var errNoDatabase = errors.New("DATABASE_URL is required: this context runs on real Postgres")

// errNoAgentURL is the boot error of a missing A2A_AGENT_URL: the Agent Card
// must advertise a URL the clients can dial, so there is no default.
var errNoAgentURL = errors.New("A2A_AGENT_URL is required: the absolute http(s) URL clients dial, advertised on the Agent Card")

// agentURLFromEnv validates A2A_AGENT_URL: absolute, http or https, with a
// host. It is checked before anything else starts, so a misconfigured pod
// refuses to boot instead of advertising a card nobody can dial.
func agentURLFromEnv(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errNoAgentURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("A2A_AGENT_URL must be an absolute http(s) URL with a host, got %q", raw)
	}
	return raw, nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("a2a server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Refuse a missing or malformed card URL before anything else starts.
	agentURL, err := agentURLFromEnv(os.Getenv("A2A_AGENT_URL"))
	if err != nil {
		return err
	}

	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Telemetry comes up before the router is built (otelchimetric binds the
	// global MeterProvider at construction) and is flushed last.
	tel := loadTelemetryEnv(os.Getenv)
	flushTelemetry, err := startTelemetry(logger, tel)
	if err != nil {
		return err
	}
	defer flushTelemetry()

	databaseURL := os.Getenv("DATABASE_URL")
	deps, closeAdapters, err := buildDeps(context.Background(), logger, databaseURL, getenv("MIGRATIONS_DATABASE_URL", databaseURL))
	if err != nil {
		return err
	}
	defer closeAdapters()

	logger.Info("a2a server starting",
		"version", getenv("SERVICE_VERSION", "dev"), "environment", getenv("ENVIRONMENT", "dev"))

	srv := &http.Server{
		Addr: getenv("A2A_ADDR", ":8093"),
		Handler: newRouter(
			inbounda2a.Handler(inbounda2a.NewExecutor(deps)),
			inbounda2a.Card(inbounda2a.Config{AgentURL: agentURL, Version: getenv("SERVICE_VERSION", "dev")}),
			logger, tel.serviceName),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return serveA2A(logger, srv)
}

// buildDeps migrates the database, connects a pgx pool and wires the three read
// use cases over the Postgres repositories. The returned close func releases
// the pool. An empty databaseURL is a boot error: there is no fallback
// runtime.
//
// Like cmd/api (and every sibling's cmd/mcp) this binary runs the idempotent
// embedded migrations on start, so it can boot against a fresh database
// before cmd/api has; golang-migrate's advisory lock makes concurrent starts
// safe. migrationsDatabaseURL is used ONLY for that step (a DIRECT Postgres
// DSN where DATABASE_URL points at PgBouncer, which cannot honour the
// session-scoped advisory lock); the runtime pool always uses databaseURL.
func buildDeps(ctx context.Context, logger *slog.Logger, databaseURL, migrationsDatabaseURL string) (inbounda2a.Deps, func(), error) {
	noop := func() {}
	if databaseURL == "" {
		return inbounda2a.Deps{}, noop, errNoDatabase
	}
	// The first outbound dial of an injected pod can be reset (native
	// sidecars), so migrations and the first ping retry with backoff; on
	// exhaustion the process refuses to boot.
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error {
		return postgres.RunMigrations(migrationsDatabaseURL)
	}); err != nil {
		return inbounda2a.Deps{}, noop, err
	}
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return inbounda2a.Deps{}, noop, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return inbounda2a.Deps{}, noop, err
	}
	logger.Info("postgres repositories configured")
	nodes := postgres.NewNodeRepo(pool)
	return inbounda2a.Deps{
		GetNode:            usecases.NewGetNode(nodes),
		ListNodes:          usecases.NewListNodes(nodes),
		ResolveServiceArea: usecases.NewResolveServiceArea(postgres.NewServiceAreaRepo(pool)),
	}, pool.Close, nil
}

// serveA2A runs srv until it fails or SIGINT/SIGTERM arrives, then drains it
// gracefully.
func serveA2A(logger *slog.Logger, srv *http.Server) error {
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("a2a server listening (JSON-RPC over HTTP)", "addr", srv.Addr)
		serverErr <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newLogger builds the process-wide JSON logger. LOG_LEVEL maps
// debug|info|warn|warning|error (case-insensitive; "warning" is accepted
// like "warn" fleet-wide), defaulting to Info. The handler is wrapped so a
// record written with a span-carrying context also carries trace_id/span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
