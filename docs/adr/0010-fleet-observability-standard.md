# ADR 0010: Adopt the fleet observability standard (logs, metrics, traces)

## Status

Accepted (2026-10-26).

## Context

node-topology logged JSON with `log/slog` and nothing else: no trace or span id
on a log line, no request id, no request log, no metrics, no traces, no
`traceparent` on the events it publishes, and no OpenTelemetry settings in the
Helm chart. The warehouse-systems services (inventory-storage, wes-work-planning)
already share one pipeline and one convention, recorded in their ADRs "standard
metrics convention" (inventory-storage ADR 0016, wes-work-planning ADR 0013):

- pods export OTLP/gRPC to the in-cluster Collector
  (`otel-collector.observability.svc.cluster.local:4317`), which feeds Jaeger
  (traces) and a Prometheus exporter (metrics);
- logs stay on stdout as JSON for Alloy and Loki.

Without the same wiring a node registration could not be followed from the REST
call to the event it publishes, a log line could not be joined to its trace, and
no dashboard could show request rate, errors or latency. The alternatives weighed
were a service-specific setup (rejected: the point is that every repo answers the
same query the same way) and the OTel Go auto-instrumentation agents (rejected:
they cannot follow a trace across `segmentio/kafka-go`, which has no
instrumentation, nor name spans by the chi route pattern).

## Decision

Tier 1 (identical in every repo) and Tier 2 (this context's business metric):

1. **Telemetry adapter.** `internal/adapters/outbound/telemetry` holds `Setup`
   (OTLP/gRPC traces and metrics, W3C TraceContext + Baggage propagator, Go
   runtime metrics; exporters dial lazily, so no Collector means dropped
   telemetry, never a blocked boot or request; returns a shutdown func) and
   `WithTraceContext` (stamps `trace_id`/`span_id` on a log record written with
   a span-carrying context). Only the composition roots (`cmd/api`, `cmd/mcp`)
   import it, plus the adapters; `internal/domain` and `internal/application`
   never import OpenTelemetry.
2. **Logger.** `slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, ...)))`,
   `LOG_LEVEL`, uppercase levels. Code on a request path uses the `*Context`
   slog variants; the bare variants have no span.
3. **HTTP middleware order** on both chi routers (REST and MCP):
   `RequestID` -> `otelchi.Middleware(serviceName, WithChiRoutes(r))` ->
   `otelchimetric.NewServerRequestDuration` -> `RequestLogger` -> `Recoverer`;
   the existing `rejectNUL` and Idempotency-Key middleware stay after them.
   `telemetry.Setup` runs before the router is built (otelchimetric binds the
   global MeterProvider at construction) and is flushed last on shutdown. The
   request line carries `method`, `route` (the chi pattern, never the raw path,
   which holds ids), `status`, `duration_ms`, `bytes` and `request_id`; 5xx is
   ERROR, 4xx WARN, the rest INFO; `/healthz` and `/readyz` are not logged.
   `RequestLogger` lives in `internal/adapters/inbound/requestlog`, not in the
   REST adapter, because `cmd/mcp` may not import the REST adapter
   (`TestMCPServerIsReadOnly`) and both routers must log identically.
4. **Traces over Kafka.** `OutboxRepo.Append` stamps `traceparent`/`tracestate`
   from the active span into the stored outbox headers, inside the transaction
   that writes the aggregate (`internal/adapters/outbound/postgres/outbox_trace.go`).
   The relay publishes the headers unchanged as Kafka headers, so a consumer in
   another service can continue the trace. Only extra headers are added: the
   CloudEvents attributes and body, and the encoder's output, are unchanged
   (ADR 0004). This service consumes no topic (ADR 0003), so it has no consumer
   span; that part of the standard is skipped here.
5. **Environment contract.** `cmd/api` and `cmd/mcp` read `OTEL_SERVICE_NAME`
   (defaults `node-topology` and `node-topology-mcp`),
   `OTEL_EXPORTER_OTLP_ENDPOINT` (default `telemetry.DefaultEndpoint`, a bare
   `host:port` or a URL), `SERVICE_VERSION`, `ENVIRONMENT` (read by `Setup`) and
   `LOG_LEVEL`.
6. **Chart.** `otel.enabled`, `otel.endpoint`, `otel.serviceName` render
   `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_SERVICE_NAME` (`<name>-mcp` for the
   mcp component); `SERVICE_VERSION` is the image tag, or the chart appVersion
   when empty, for the api and the mcp component (`mcp.serviceVersion` still
   overrides); the api also renders `ENVIRONMENT` from `environment`. `extraEnv`
   may not set any of the four. `charts/node-topology/tests/render_test.sh`
   asserts it and scans `cmd/` and the telemetry adapter for the variables the
   binaries read.
7. **Tier 2 business metric.** One counter, `topology.nodes.registered`, unit
   `{node}`, attribute `outcome` (`registered` | `rejected`) and, on a rejection,
   `reason` (the problem slug the REST adapter answers with:
   `vendor-node-code-taken`, `operator-not-allowed-for-node-type`,
   `subtype-not-allowed`, `unknown-region`, `validation-failed`,
   `concurrent-update`, `internal-error`). `ports.NodeMetrics` is the application
   port; `RegisterNode` calls it once per registration attempt, after its
   retry on a concurrent update; the OTel implementation is in the telemetry
   adapter and is injected from `cmd/api`; a nil `Deps.Metrics` is a no-op. It
   follows the fleet convention: dotted lowercase name, one counter with an
   `outcome` attribute rather than two counters, UCUM unit, a description that
   says what a human should conclude when it moves.

   Why registration: it is the one command through which the network grows and
   the one that external onboarding clients (vendors) drive, so its refusals
   carry the signal a human acts on (a rising `vendor-node-code-taken` or
   `operator-not-allowed-for-node-type` is a client sending rejected data; a
   rising `internal-error` is the service failing). Activation, suspension and
   lane or service-area changes are operator-driven and low volume, and
   `http.server.request.duration` already counts them per route and status.

Assumption (recorded as the standard asks): where the reference and this repo
differ, the reference shape wins and the repo's own names are kept (module
path, `mcp.environment` stays `dev`, the chart's existing `mcp.serviceVersion`
key remains an override). No MCP-specific business counter is added.

Not part of this ADR: dashboards and alert rules, which live in warehouse-infra
and are generated per deployed context from the instrument names above.

## Consequences

- A request, its log lines and the events it enqueued share one trace id;
  `trace_id` in Loki jumps to Jaeger, and a downstream consumer can parent its
  span on the `traceparent` header.
- Request rate, errors and duration come from `http.server.request.duration`
  (otelchimetric) with no hand-rolled counter; onboarding health from
  `topology.nodes.registered`.
- Every new log call on a request path must use a `*Context` variant, or its
  line carries no trace id.
- The trace parent of a published event is the span that was active at the
  outbox write (the REST request), not a separate publish span: the relay is
  asynchronous and adds no span of its own.
- Outbox rows written outside a span (or by a path other than `Append`) carry no
  `traceparent`.
