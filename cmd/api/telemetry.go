package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/claudioed/node-topology/internal/adapters/outbound/telemetry"
)

// The observability part of the environment (ADR 0010). ENVIRONMENT
// (deployment.environment.name) is read by telemetry.Setup itself.
const (
	envServiceName    = "OTEL_SERVICE_NAME"
	envServiceVersion = "SERVICE_VERSION"
	envOTLPEndpoint   = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

// defaultServiceName is the OTEL_SERVICE_NAME default: the name every span,
// metric and Loki `app` label of this binary carries.
const defaultServiceName = "node-topology"

// telemetryFlushTimeout bounds the final export attempt. Without a deadline the
// exporter would retry against an unreachable Collector and stretch a shutdown
// out well past what an orchestrator will wait for.
const telemetryFlushTimeout = 5 * time.Second

// telemetryEnv is the observability part of the environment.
//
//	OTEL_SERVICE_NAME            service.name of spans and metrics (default node-topology)
//	OTEL_EXPORTER_OTLP_ENDPOINT  Collector OTLP/gRPC endpoint, host:port or URL (default telemetry.DefaultEndpoint)
//	SERVICE_VERSION              service.version (default telemetry.DefaultServiceVersion)
//	ENVIRONMENT                  deployment.environment.name, read by telemetry.Setup
type telemetryEnv struct {
	serviceName    string
	serviceVersion string
	otlpEndpoint   string
}

func loadTelemetryEnv(getenv func(string) string) telemetryEnv {
	return telemetryEnv{
		serviceName:    orDefault(getenv(envServiceName), defaultServiceName),
		serviceVersion: orDefault(getenv(envServiceVersion), telemetry.DefaultServiceVersion),
		otlpEndpoint:   orDefault(getenv(envOTLPEndpoint), telemetry.DefaultEndpoint),
	}
}

// newLogger builds the JSON logger; LOG_LEVEL debug|info|warn|warning|error
// (case-insensitive). The handler is wrapped so a record written with a
// span-carrying context (the *Context slog variants) also carries trace_id and
// span_id.
func newLogger(level string) *slog.Logger {
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: newLogLevel(level)})))
}

// startTelemetry brings OpenTelemetry up and returns the flush func to defer.
// Export is non-blocking: an unreachable Collector costs telemetry, never
// availability. A failed flush is logged, never returned: turning that into a
// non-zero exit would make every clean shutdown look like a crash.
//
// It must run before the HTTP router is built: otelchimetric binds the global
// MeterProvider when the router is constructed.
func startTelemetry(logger *slog.Logger, cfg telemetryEnv) (func(), error) {
	shutdown, err := telemetry.Setup(context.Background(), cfg.serviceName, cfg.serviceVersion, cfg.otlpEndpoint)
	if err != nil {
		return nil, err
	}
	logger.Info("telemetry configured", "service_name", cfg.serviceName, "otlp_endpoint", cfg.otlpEndpoint)
	return func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
		defer cancel()
		if err := shutdown(flushCtx); err != nil {
			logger.Warn("telemetry flush failed on shutdown", "error", err)
		}
	}, nil
}
