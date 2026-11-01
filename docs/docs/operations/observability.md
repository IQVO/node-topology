---
id: observability
title: Observability
sidebar_label: Observability
description: "OpenTelemetry instruments, spans, log fields, exporter variables and the dashboards and alerts that exist for Node Topology."
---

# Observability

The service follows the fleet observability standard ([ADR 0010](/docs/adr/0010-fleet-observability-standard)): traces and metrics leave the pod over OTLP/gRPC to the OpenTelemetry Collector (Jaeger for traces, Prometheus for metrics), and logs are JSON on stdout, shipped by Alloy to Loki. Export never blocks: gRPC exporters connect lazily, so a Collector that is down or absent costs telemetry and nothing else. Source: `internal/adapters/outbound/telemetry/telemetry.go`.

## Exporter and resource variables

All three binaries (`cmd/api`, `cmd/mcp`, `cmd/a2a`) read the same four variables; defaults and the per-binary service names are in [Configuration](/docs/operations/configuration).

| Variable | Effect |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Collector OTLP/gRPC endpoint. Default `localhost:4317`; a bare `host:port` becomes `http://host:port`, a full URL is used as is. The chart sets `otel-collector.observability.svc.cluster.local:4317`. |
| `OTEL_SERVICE_NAME` | `service.name`: `node-topology`, `node-topology-mcp`, `node-topology-a2a`. Also the `app` label the dashboard uses for logs and the `service_name` label for metrics. |
| `SERVICE_VERSION` | `service.version`. |
| `ENVIRONMENT` | `deployment.environment.name`. |

Resource attributes also include `telemetry.sdk.*` and anything in the standard `OTEL_RESOURCE_ATTRIBUTES`. Metrics are pushed every 15 s (PeriodicReader). The W3C `traceparent` and baggage propagators are installed globally. The SDK's own diagnostics are routed through `slog` at DEBUG, so an unreachable Collector shows up only with `LOG_LEVEL=debug` (`opentelemetry error`).

## Metrics

| Instrument | Type | Unit | Attributes | Emitted by |
| --- | --- | --- | --- | --- |
| `topology.nodes.registered` | Int64 counter | `{node}` | `outcome` (`registered` or `rejected`); `reason` on rejections | `cmd/api`, scope `github.com/claudioed/node-topology` (`node_metrics.go`) |
| `http.server.request.duration` | Float64 histogram | `s` | `http.method`, `http.scheme`, `http.route` (bucket bounds 5 ms to 10 s) | all three binaries, through `otelchimetric` |
| Go runtime metrics (`go.*`: goroutines, memory, GC) | various | various | none of ours | all three binaries, `go.opentelemetry.io/contrib/instrumentation/runtime` |

`topology.nodes.registered` is the Tier 2 business metric: one increment per `RegisterNode` attempt. `reason` is the problem slug the REST adapter would answer with: `vendor-node-code-taken`, `operator-not-allowed-for-node-type`, `subtype-not-allowed`, `unknown-region`, `concurrent-update`, `validation-failed`, or `internal-error` for anything else (`internal/application/usecases/register_metrics.go`). Read it this way: a rising `vendor-node-code-taken` or `operator-not-allowed-for-node-type` means onboarding clients send registrations the network rules refuse; a rising `internal-error` or `concurrent-update` means the service itself is in trouble. Prometheus exposes it as `topology_nodes_registered_total`.

The instrument is registered after telemetry setup, so it binds to the real provider; before that, the global provider is a no-op and tests record at no cost.

There is no instrument for the outbox backlog, relay passes or idempotency replays. Use the SQL in the [runbook](/docs/operations/runbook#the-outbox-relay) and the log messages below.

## Traces

| Span | Created by | Attributes |
| --- | --- | --- |
| Server span named after the chi route pattern (for example `/nodes/{nodeId}`) | `otelchi` middleware on every router (REST, MCP, A2A), one name per route, never per id | `http.route`, `http.status_code` |
| `a2a.skill <skill>` | A2A executor (`internal/adapters/inbound/a2a/skills.go`), tracer scope `github.com/claudioed/node-topology/internal/adapters/inbound/a2a` | `a2a.skill`; status `Error` with the problem detail on a failed skill |

Context propagation across Kafka: when a write raises events, the active span context is injected into the outbox row's headers in the same transaction (`internal/adapters/outbound/postgres/outbox_trace.go`), so the relay publishes `traceparent` as an ordinary Kafka header and a consumer can parent its span on the request that enqueued the event. In the quickstart run the broker showed `traceparent:00-0d982318fd784e3ffdd2ba0e6338dc7c-...-01` next to `content-type:application/cloudevents+json; charset=UTF-8`.

## Logs

JSON on stdout through `slog`; the handler is wrapped so a record written with a span-carrying context also carries `trace_id` and `span_id`. Records written without a span (start-up, shutdown, relay) carry neither. `LOG_LEVEL` sets the threshold.

Access log, one line per request (`internal/adapters/inbound/requestlog/requestlog.go`; `/healthz` and `/readyz` are skipped), captured from a real run:

```json
{"time":"2026-10-31T20:36:17.266576-03:00","level":"WARN","msg":"http request","method":"PUT","route":"/nodes/{nodeId}/operational-profile","status":409,"duration_ms":12,"bytes":251,"request_id":"claudioeds-MacBook-Pro.local/54CKeMtyC6-000038","trace_id":"a8c301bf57fa23cc52d6fc73451e627d","span_id":"7d0feedf61226b92"}
```

| Field | Meaning |
| --- | --- |
| `msg` | always `http request` |
| `level` | `INFO` below 400, `WARN` for 4xx, `ERROR` for 5xx |
| `method`, `route` | HTTP method and the matched route pattern (never the raw path; unmatched requests log `unmatched`) |
| `status`, `duration_ms`, `bytes` | status, latency in ms, response bytes |
| `request_id` | chi request id |
| `trace_id`, `span_id` | present when a span is active |

Other messages worth searching for: `telemetry configured` (fields `service_name`, `otlp_endpoint`), `postgres adapters configured`, `outbox relay running` (`interval`, `max_attempts`, `topic`), `outbox relay pass failed`, `outbox relay: row reached the attempt cap and is excluded from future claims; re-arm it with attempts = 0 after fixing the cause` (`outbox_id`, `topic`, `event_type`, `key`, `attempts`, `max_attempts`, `error`), `readiness: dependency is not ready` (`dependency`, `error`), `shutdown: readiness flipped to not-ready; waiting for traffic to drain`, `panic in handler`, `telemetry flush failed on shutdown`, and `invalid OUTBOX_MAX_ATTEMPTS; using the default` style warnings for malformed settings.

## Dashboards and alerts

Only what exists in `warehouse-infra` on `develop`:

- **Dashboard** `warehouse-node-topology` ("Warehouse — Node Topology", `terraform/dashboards/contexts/node-topology.json`): Kong request rate, 5xx ratio and latency; request rate and p95 by route and process (`http_server_request_duration_seconds{service_name=~"node-topology.*"}`); `topology_nodes_registered_total` by `outcome`; Go goroutines and memory; Loki live stream, log volume by level, and errors and warnings for `{app="node-topology"}`.
- **Alert rules** (`terraform/alerting/warehouse-alert-rules.json`): multi-window burn-rate rules on the Kong 5xx ratio and on upstream latency for routes matching `node-topology(-mcp\|-reports)?`.
- **Not present:** alerts on the outbox backlog, the attempt cap, `/readyz`, or on `topology.nodes.registered` rejections.
