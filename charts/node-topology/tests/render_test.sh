#!/usr/bin/env bash
# Chart render assertions: the chart deploys the REAL runtime of this context
# (Postgres and Kafka are the only runtime, no in-memory mode), so the
# mandatory configuration has no silent default, no *_MODE variable is ever
# rendered, this service (which consumes no topic) has no consumer group, and
# the env the binary reads is the env the chart renders.
# Run: bash charts/node-topology/tests/render_test.sh   (needs helm)
set -euo pipefail

chart_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo_dir="$(cd "$chart_dir/../.." && pwd)"
release=node-topology
# Dummy values, no password: the chart refuses to render without a database
# source and without Kafka brokers.
base=(--set database.url=postgres://u@example.invalid:5432/db --set kafka.brokers=kafka.example.invalid:9092)
# Every optional value and component set (ct lint's all-components values
# file), so every env var the binaries can read is rendered.
everything=(-f "$chart_dir/ci/all-components-values.yaml")

fail() { echo "FAIL: $*" >&2; exit 1; }

render() { helm template "$release" "$chart_dir" "$@"; }
# render_only <template> <helm args...>
render_only() { local t="$1"; shift; render "$@" --show-only "templates/$t"; }

configmap="$(render_only configmap.yaml "${base[@]}")"
deployment="$(render_only deployment.yaml "${base[@]}")"
configmap_name="$(awk '/^  name:/ {print $2; exit}' <<<"$configmap")"
[[ -n "$configmap_name" ]] || fail "could not read the ConfigMap name"

# --- the ConfigMap is mounted ------------------------------------------------
grep -A3 'envFrom:' <<<"$deployment" | grep -q 'configMapRef:' \
  || fail "Deployment does not mount the ConfigMap via envFrom"
grep -A3 'envFrom:' <<<"$deployment" | grep -q "name: $configmap_name" \
  || fail "Deployment envFrom does not reference ConfigMap $configmap_name"

# --- mandatory configuration is rendered from values -------------------------
env_value() { # env_value <rendered> <NAME>
  grep -A1 "name: $2\$" <<<"$1" | grep -m1 'value:' | sed -E 's/.*value: "?//; s/"$//'
}
[[ "$(env_value "$deployment" KAFKA_BROKERS)" == "kafka.example.invalid:9092" ]] || fail "KAFKA_BROKERS is not rendered from kafka.brokers"
grep -A4 'name: DATABASE_URL$' <<<"$deployment" | grep -q 'secretKeyRef:' || fail "DATABASE_URL must come from a Secret (secretKeyRef)"
grep -A6 'name: MIGRATIONS_DATABASE_URL$' <<<"$deployment" | grep -q 'optional: true' || fail "MIGRATIONS_DATABASE_URL must be an optional secretKeyRef"
grep -q "^  KAFKA_BROKERS: " <<<"$configmap" || fail "the ConfigMap does not carry KAFKA_BROKERS"
if grep -qE '^  (MIGRATIONS_)?DATABASE_URL:' <<<"$configmap"; then fail "a DSN must never land in the ConfigMap"; fi

# --- no consumer group, no mode, ever ----------------------------------------
all_docs="$(render "${base[@]}" "${everything[@]}")"
# Comments are rendered too: assert on the manifests, not on the explanations.
all_nocomment="$(grep -vE '^[[:space:]]*#' <<<"$all_docs")"
if grep -qE '_CONSUMER_GROUP' <<<"$all_nocomment"; then fail "this service consumes no topic: no *_CONSUMER_GROUP may be rendered"; fi
if grep -qE '(name: |^  )[A-Z_]+_MODE:?$|^  [A-Z_]+_MODE: ' <<<"$all_nocomment"; then fail "a *_MODE variable is rendered; the binary reads none"; fi
if grep -qiE 'value: "?permissive' <<<"$all_docs"; then fail "permissive is rendered"; fi
if grep -q 'DATABASE_URL: ""' <<<"$all_docs"; then fail "an empty DATABASE_URL must never be rendered"; fi
if grep -vE '^[[:space:]]*#' "$chart_dir/values.yaml" | grep -qiE '_CONSUMER_GROUP|consumerGroup'; then
  fail "values.yaml must not define a consumer group (the service consumes no topic)"
fi
# The binary agrees: it has no consumer group either.
if grep -rqE '_CONSUMER_GROUP' --include='*.go' "$repo_dir/cmd/api"; then
  fail "cmd/api now reads a *_CONSUMER_GROUP: add a config.* value, a required-check and a refusal test to the chart"
fi

# --- probes: /healthz liveness (and startup), /readyz readiness --------------
probe_block() { # probe_block <probeName> <rendered deployment>
  awk -v p="$1:" '$1 == p {f=1; next} f && /^          [a-zA-Z]/ {exit} f {print}' <<<"$2"
}
grep -q 'path: /healthz' <<<"$(probe_block livenessProbe "$deployment")" || fail "livenessProbe must hit /healthz"
grep -q 'path: /healthz' <<<"$(probe_block startupProbe "$deployment")" || fail "startupProbe must hit /healthz"
readiness="$(probe_block readinessProbe "$deployment")"
grep -q 'path: /readyz' <<<"$readiness" || fail "readinessProbe must hit /readyz"
grep -q 'initialDelaySeconds: [1-9]' <<<"$readiness" || fail "readinessProbe needs an initialDelaySeconds"
grep -q 'periodSeconds: [1-9]' <<<"$readiness" || fail "readinessProbe needs a periodSeconds"
grep -q 'timeoutSeconds: [3-9]' <<<"$readiness" || fail "readinessProbe timeoutSeconds must exceed the dependency check bound"
grep -q 'failureThreshold: [3-9]' <<<"$readiness" || fail "readinessProbe needs a failureThreshold of at least 3"
probe_block livenessProbe "$deployment" | grep -q 'failureThreshold: [3-9]' || fail "livenessProbe needs a failureThreshold of at least 3"

# --- container hardening ------------------------------------------------------
grep -q 'runAsNonRoot: true' <<<"$deployment" || fail "the pod must run as non-root"
grep -q 'readOnlyRootFilesystem: true' <<<"$deployment" || fail "the root filesystem must be read-only"
grep -q 'allowPrivilegeEscalation: false' <<<"$deployment" || fail "privilege escalation must be disabled"
grep -A2 'capabilities:' <<<"$deployment" | grep -q 'ALL' || fail "capabilities must drop ALL"
grep -A1 'seccompProfile:' <<<"$deployment" | grep -q 'type: RuntimeDefault' || fail "seccompProfile must be RuntimeDefault"

# --- graceful shutdown room ----------------------------------------------------
# cmd/api serveUntilSignal: SHUTDOWN_DRAIN_DELAY (5s) + HTTP drain 10s
# (shutdownTimeout) + outbox relay 10s (workerDrainTimeout) = 25s.
grep -qE 'shutdownTimeout +=  *10 \* time.Second' "$repo_dir/cmd/api/config.go" \
  || fail "shutdownTimeout in cmd/api/config.go changed: re-derive the grace period floor in the chart"
grep -qE 'workerDrainTimeout +=  *10 \* time.Second' "$repo_dir/cmd/api/config.go" \
  || fail "workerDrainTimeout in cmd/api/config.go changed: re-derive the grace period floor in the chart"
grep -qE 'defaultShutdownDrain +=  *5 \* time.Second' "$repo_dir/cmd/api/config.go" \
  || fail "defaultShutdownDrain in cmd/api/config.go changed: re-derive the grace period floor in the chart"
grace="$(awk '/terminationGracePeriodSeconds:/ {print $2; exit}' <<<"$deployment")"
(( grace > 25 )) || fail "terminationGracePeriodSeconds is $grace; want more than 25 (drain delay + HTTP + relay)"

# --- optional values reach the ConfigMap and the Deployment -------------------
tuned_args=("${base[@]}" --set config.outboxRelayInterval=7s --set config.outboxMaxAttempts=5 \
  --set config.shutdownDrainDelay=3s --set config.httpAddr=:9090 --set config.logLevel=debug)
tuned_cm="$(render_only configmap.yaml "${tuned_args[@]}")"
tuned_dep="$(render_only deployment.yaml "${tuned_args[@]}")"
for kv in 'OUTBOX_RELAY_INTERVAL: "7s"' 'OUTBOX_MAX_ATTEMPTS: "5"' 'SHUTDOWN_DRAIN_DELAY: "3s"' \
  'HTTP_ADDR: ":9090"' 'LOG_LEVEL: "debug"'; do
  grep -q "$kv" <<<"$tuned_cm" || fail "the ConfigMap does not render $kv"
done
for pair in OUTBOX_RELAY_INTERVAL=7s OUTBOX_MAX_ATTEMPTS=5 SHUTDOWN_DRAIN_DELAY=3s \
  HTTP_ADDR=:9090 LOG_LEVEL=debug; do
  [[ "$(env_value "$tuned_dep" "${pair%%=*}")" == "${pair#*=}" ]] || fail "the Deployment does not render ${pair%%=*}=${pair#*=}"
done
# Unset optional values are not rendered at all, so the binary's defaults apply.
for var in OUTBOX_RELAY_INTERVAL SHUTDOWN_DRAIN_DELAY; do
  if grep -q "$var" <<<"$deployment"; then fail "$var must not be rendered while its value is empty"; fi
done
# The relay attempt cap defaults to 20 and 0 (unlimited) is rendered as is.
grep -q 'OUTBOX_MAX_ATTEMPTS: "20"' <<<"$configmap" || fail "OUTBOX_MAX_ATTEMPTS must default to 20"
unlimited="$(render_only configmap.yaml "${base[@]}" --set config.outboxMaxAttempts=0)"
grep -q 'OUTBOX_MAX_ATTEMPTS: "0"' <<<"$unlimited" || fail "config.outboxMaxAttempts=0 is not rendered"

# --- image tag -------------------------------------------------------------------
# The release tags the image v<version> and packages the chart with
# appVersion=<version>: an empty image.tag must resolve to v<appVersion>.
grep -qE 'image: "ghcr.io/iqvo/node-topology:v[0-9]+\.[0-9]+\.[0-9]+"' <<<"$deployment" \
  || fail "the default image must be ghcr.io/iqvo/node-topology:v<appVersion>"
pinned="$(render_only deployment.yaml "${base[@]}" --set image.tag=local)"
grep -q 'image: "ghcr.io/iqvo/node-topology:local"' <<<"$pinned" || fail "an explicit image.tag must be used verbatim"

# --- chart vs binary ------------------------------------------------------------
# Every env var cmd/api reads must be rendered into the Deployment when every
# optional value is set. The names come from the binary's own constants:
# `env... = "NAME"` in cmd/api (non-test) and `Env... = "NAME"` in the Postgres
# adapter (DATABASE_URL, MIGRATIONS_DATABASE_URL), plus any getenv("NAME")
# literal, so a new variable fails this test until the chart renders it.
binary_env() {
  local f
  for f in "$repo_dir"/cmd/api/*.go "$repo_dir"/internal/adapters/outbound/postgres/config.go \
    "$repo_dir"/internal/adapters/outbound/telemetry/telemetry.go; do
    case "$f" in *_test.go) continue ;; esac
    grep -hoE '^[[:space:]]*[Ee]nv[A-Za-z]*[[:space:]]*=[[:space:]]*"[A-Z][A-Z0-9_]*"' "$f" || true
    grep -hoE '[Gg]etenv\("[A-Z][A-Z0-9_]*"' "$f" || true
  done | sed -E 's/.*"([A-Z0-9_]+)".*/\1/' | sort -u
}
# Word splitting is safe: the names are [A-Z0-9_] only (bash 3.2 has no mapfile).
# shellcheck disable=SC2207
api_vars=($(binary_env))
(( ${#api_vars[@]} >= 8 )) || fail "found only ${#api_vars[@]} env names in cmd/api; the scan is looking in the wrong place"
full_deployment="$(render_only deployment.yaml "${everything[@]}")"
for var in "${api_vars[@]}"; do
  grep -q "name: $var\$" <<<"$full_deployment" || fail "the Deployment does not render $var, which the binary reads"
done
# Every key the ConfigMap renders is a variable cmd/api reads (no dead keys),
# and so is every env var the Deployment renders from a chart value.
full_configmap="$(render_only configmap.yaml "${everything[@]}")"
known=" ${api_vars[*]} "
while read -r key; do
  [[ "$known" == *" $key "* ]] || fail "ConfigMap key $key is read by no binary"
done < <(awk '/^data:/ {d=1; next} d && /^  [A-Z]/ {sub(/:.*/, ""); sub(/^  /, ""); print}' <<<"$full_configmap")
while read -r key; do
  [[ "$known" == *" $key "* ]] || fail "Deployment env $key is read by no binary"
done < <(grep -E '^            - name: [A-Z]' <<<"$full_deployment" | sed -E 's/.*name: //')

# --- observability (ADR 0010, fleet standard item 7) --------------------------------
# The api renders the fleet pipeline's env: the Collector endpoint, the service
# name, the version (image tag, appVersion when empty) and the environment.
otel_endpoint="otel-collector.observability.svc.cluster.local:4317"
app_version="$(awk '/^appVersion:/ {gsub(/"/, "", $2); print $2}' "$chart_dir/Chart.yaml")"
[[ "$(env_value "$deployment" OTEL_EXPORTER_OTLP_ENDPOINT)" == "$otel_endpoint" ]] \
  || fail "the api Deployment does not render OTEL_EXPORTER_OTLP_ENDPOINT from otel.endpoint"
[[ "$(env_value "$deployment" OTEL_SERVICE_NAME)" == "node-topology" ]] \
  || fail "the api Deployment does not render OTEL_SERVICE_NAME=node-topology from otel.serviceName"
[[ "$(env_value "$deployment" SERVICE_VERSION)" == "$app_version" ]] || fail "SERVICE_VERSION must default to the chart appVersion"
[[ "$(env_value "$deployment" ENVIRONMENT)" == "production" ]] || fail "ENVIRONMENT must come from environment"
tagged="$(render_only deployment.yaml "${base[@]}" --set image.tag=v9.9.9 --set otel.serviceName=topology \
  --set otel.endpoint=http://collector:4317 --set environment=staging)"
[[ "$(env_value "$tagged" SERVICE_VERSION)" == "v9.9.9" ]] || fail "SERVICE_VERSION must follow image.tag"
[[ "$(env_value "$tagged" OTEL_SERVICE_NAME)" == "topology" ]] || fail "OTEL_SERVICE_NAME must follow otel.serviceName"
[[ "$(env_value "$tagged" OTEL_EXPORTER_OTLP_ENDPOINT)" == "http://collector:4317" ]] || fail "OTEL_EXPORTER_OTLP_ENDPOINT must follow otel.endpoint"
[[ "$(env_value "$tagged" ENVIRONMENT)" == "staging" ]] || fail "ENVIRONMENT must follow environment"
# otel.enabled=false renders no OTEL_* env (SERVICE_VERSION stays: it also labels the logs' build).
otel_off="$(render "${base[@]}" --set otel.enabled=false --set mcp.enabled=true --set a2a.enabled=true)"
if grep -q 'name: OTEL_' <<<"$otel_off"; then fail "otel.enabled=false must render no OTEL_* env in any component"; fi
grep -q 'name: SERVICE_VERSION$' <<<"$otel_off" || fail "SERVICE_VERSION must still render with otel.enabled=false"
# The values are chart-owned: extraEnv may not set them a second time.
for owned in OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT SERVICE_VERSION ENVIRONMENT; do
  err="$(render "${base[@]}" --set "extraEnv[0].name=$owned" --set 'extraEnv[0].value=x' 2>&1)" \
    && fail "extraEnv must not be able to set $owned"
  grep -qF "extraEnv sets $owned" <<<"$err" || fail "the refusal for extraEnv $owned does not name it: $err"
done

# --- the Secret ----------------------------------------------------------------
secret="$(render_only secret.yaml "${base[@]}")"
grep -q 'DATABASE_URL: "postgres://u@example.invalid:5432/db"' <<<"$secret" || fail "database.url does not land in the chart Secret"
existing="$(render --set database.existingSecret=my-db --set kafka.brokers=k:9092)"
if grep -q 'kind: Secret' <<<"$existing"; then fail "an existingSecret must suppress the chart Secret"; fi
grep -A4 'name: DATABASE_URL$' <<<"$existing" | grep -q 'name: my-db' || fail "an existingSecret is not referenced by DATABASE_URL"
grep -A6 'name: MIGRATIONS_DATABASE_URL$' <<<"$existing" | grep -q 'name: my-db' || fail "an existingSecret is not referenced by MIGRATIONS_DATABASE_URL"

# --- refusals: the mandatory values name themselves -------------------------------
expect_refusal() { # expect_refusal <why> <needle> <helm args...>
  local why="$1" needle="$2" err; shift 2
  if err="$(helm template "$release" "$chart_dir" "$@" 2>&1)"; then
    fail "the chart must refuse to render: $why"
  fi
  grep -qF -- "$needle" <<<"$err" || fail "the refusal for '$why' does not name '$needle':
$err"
}
expect_refusal "no database source and no brokers" "database.url or database.existingSecret"
expect_refusal "no database source" "database.url or database.existingSecret" --set kafka.brokers=k:9092
expect_refusal "no brokers" "kafka.brokers" --set database.url=postgres://u@h/db
expect_refusal "a short grace period" "terminationGracePeriodSeconds" "${base[@]}" --set terminationGracePeriodSeconds=20
expect_refusal "a grace period just under the floor" "terminationGracePeriodSeconds" "${base[@]}" --set terminationGracePeriodSeconds=29
expect_refusal "a drain delay beyond the grace period" "terminationGracePeriodSeconds" "${base[@]}" --set config.shutdownDrainDelay=30s
expect_refusal "a *_MODE through extraEnv" "*_MODE" "${base[@]}" --set 'extraEnv[0].name=KAFKA_MODE' --set 'extraEnv[0].value=permissive'
expect_refusal "a *_CONSUMER_GROUP through extraEnv" "consumes no topic" "${base[@]}" --set 'extraEnv[0].name=ORDERS_CONSUMER_GROUP' --set 'extraEnv[0].value=g'
# A larger drain delay is allowed once the grace period covers it (+20 s).
render_only deployment.yaml "${base[@]}" --set config.shutdownDrainDelay=30s --set terminationGracePeriodSeconds=50 >/dev/null \
  || fail "a 30s drain delay with a 50s grace period must render"

# --- optional components (ADR 0008 mcp, ADR 0011 a2a, ADR 0009 web remote) -----
# Every optional component is OFF by default: a default render is the api alone.
default_render="$(render "${base[@]}")"
for c in mcp a2a analytics frontend projector reports; do
  if grep -q "app.kubernetes.io/component: $c\$" <<<"$default_render"; then
    fail "the $c component must not render while its enabled flag is false"
  fi
done
grep -q 'app.kubernetes.io/component: api' <<<"$default_render" || fail "the api component is missing"
if grep -qE 'kind: (HorizontalPodAutoscaler|Ingress|HTTPRoute)' <<<"$default_render"; then
  fail "HPA, Ingress and HTTPRoute must be off by default"
fi

# binary_env_of <main.go...>: the env literals one binary reads.
binary_env_of() {
  local f
  for f in "$@"; do
    grep -hoE '([Gg]etenv|env)\("[A-Z][A-Z0-9_]*"' "$f" || true
  done | sed -E 's/.*"([A-Z0-9_]+)".*/\1/' | sort -u
}
# assert_env_rendered <rendered> <label> <vars...>
assert_env_rendered() {
  local rendered="$1" label="$2"; shift 2
  local var
  for var in "$@"; do
    grep -q "name: $var\$" <<<"$rendered" || fail "$label does not render $var, which its binary reads"
  done
}

# mcp: its own env, never the api ConfigMap, never Kafka (ADR 0008).
mcp_dep="$(render_only mcp-deployment.yaml "${base[@]}" --set mcp.enabled=true)"
grep -q 'app.kubernetes.io/component: mcp' <<<"$mcp_dep" || fail "the mcp Deployment was not rendered with mcp.enabled=true"
grep -q 'command: \["/app/mcp"\]' <<<"$mcp_dep" || fail "the mcp Deployment must run /app/mcp"
if grep -q 'configMapRef:' <<<"$mcp_dep"; then fail "mcp-deployment.yaml must not mount the api ConfigMap"; fi
if grep -Eq 'name: (KAFKA_BROKERS|OUTBOX_[A-Z_]+|[A-Z]+_CONSUMER_GROUP)$' <<<"$mcp_dep"; then
  fail "the mcp Deployment must not get Kafka/relay/consumer env (read-only, never dials Kafka)"
fi
# shellcheck disable=SC2207
mcp_vars=($(binary_env_of "$repo_dir/cmd/mcp/main.go" "$repo_dir/cmd/mcp/telemetry.go"))
(( ${#mcp_vars[@]} >= 3 )) || fail "found only ${#mcp_vars[@]} env literals in cmd/mcp/main.go; the scan is looking in the wrong place"
assert_env_rendered "$mcp_dep" "the mcp Deployment" "${mcp_vars[@]}" DATABASE_URL MIGRATIONS_DATABASE_URL \
  OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT SERVICE_VERSION ENVIRONMENT
mcp_versioned="$(render_only mcp-deployment.yaml "${base[@]}" --set mcp.enabled=true --set mcp.serviceVersion=dev)"
[[ "$(env_value "$mcp_versioned" SERVICE_VERSION)" == "dev" ]] || fail "SERVICE_VERSION must come from mcp.serviceVersion"
[[ "$(env_value "$mcp_dep" SERVICE_VERSION)" == "$app_version" ]] || fail "the mcp SERVICE_VERSION must default to the image tag / appVersion, like the api"
[[ "$(env_value "$mcp_dep" OTEL_SERVICE_NAME)" == "node-topology-mcp" ]] || fail "the mcp component must export as <otel.serviceName>-mcp"
[[ "$(env_value "$mcp_dep" OTEL_EXPORTER_OTLP_ENDPOINT)" == "$otel_endpoint" ]] || fail "the mcp component must export to otel.endpoint"
mcp_named="$(render_only mcp-deployment.yaml "${base[@]}" --set mcp.enabled=true --set otel.serviceName=topology)"
[[ "$(env_value "$mcp_named" OTEL_SERVICE_NAME)" == "topology-mcp" ]] || fail "the mcp service name must follow otel.serviceName"
[[ "$(env_value "$mcp_dep" ENVIRONMENT)" == "dev" ]] || fail "ENVIRONMENT must come from mcp.environment"

# a2a: its own env, never the api ConfigMap, never Kafka, READ-ONLY (ADR 0011).
a2a_dep="$(render_only a2a-deployment.yaml "${base[@]}" --set a2a.enabled=true)"
grep -q 'app.kubernetes.io/component: a2a' <<<"$a2a_dep" || fail "the a2a Deployment was not rendered with a2a.enabled=true"
grep -q 'command: \["/app/a2a"\]' <<<"$a2a_dep" || fail "the a2a Deployment must run /app/a2a"
if grep -q 'configMapRef:' <<<"$a2a_dep"; then fail "a2a-deployment.yaml must not mount the api ConfigMap"; fi
if grep -Eq 'name: (KAFKA_BROKERS|EVENT_PUBLISHER|OUTBOX_[A-Z_]+|[A-Z]+_CONSUMER_GROUP)$' <<<"$a2a_dep"; then
  fail "the a2a Deployment must not get Kafka/publisher/relay/consumer env (read-only agent: no write skill, never dials Kafka)"
fi
# shellcheck disable=SC2207
a2a_vars=($(binary_env_of "$repo_dir/cmd/a2a/main.go" "$repo_dir/cmd/a2a/telemetry.go"))
(( ${#a2a_vars[@]} >= 4 )) || fail "found only ${#a2a_vars[@]} env literals in cmd/a2a; the scan is looking in the wrong place"
assert_env_rendered "$a2a_dep" "the a2a Deployment" "${a2a_vars[@]}" DATABASE_URL MIGRATIONS_DATABASE_URL \
  A2A_ADDR A2A_AGENT_URL OTEL_SERVICE_NAME OTEL_EXPORTER_OTLP_ENDPOINT SERVICE_VERSION ENVIRONMENT
# Every variable cmd/a2a's boot errors name as REQUIRED ("<VAR> is required") is
# rendered, derived from the Go source so a new mandatory variable fails until
# the chart has it. The regex allows digits: A2A_AGENT_URL is not A_AGENT_URL.
a2a_required="$(grep -ho '[A-Z][A-Z0-9_]\{2,\} is required' "$repo_dir"/cmd/a2a/*.go | sed -E 's/ is required//' | sort -u || true)"
[[ -n "$a2a_required" ]] || fail "found no mandatory variables for cmd/a2a; the scan is looking in the wrong place"
for var in $a2a_required; do
  grep -q "name: $var\$" <<<"$a2a_dep" || fail "the a2a Deployment does not render $var, which cmd/a2a requires"
done
[[ "$(env_value "$a2a_dep" A2A_ADDR)" == ":8093" ]] || fail "the a2a agent must listen on :8093 (8080 is the api, 8090 the mcp server)"
# The Agent Card must advertise a dialable absolute URL: it defaults to the
# release's in-cluster Service DNS (namespace-derived, so pattern-match) and
# stays overridable. `|| true`: a missing variable must reach the fail message
# below, not abort silently under set -e.
a2a_url="$(env_value "$a2a_dep" A2A_AGENT_URL || true)"
[[ "$a2a_url" =~ ^http://[a-z0-9.-]+-a2a\.[a-z0-9-]+\.svc\.cluster\.local:8093/$ ]] || fail "A2A_AGENT_URL default is not the in-cluster Service DNS: $a2a_url"
a2a_override="$(render_only a2a-deployment.yaml "${base[@]}" --set a2a.enabled=true --set a2a.agentUrl=http://gw.example/api/a2a/node-topology/)"
[[ "$(env_value "$a2a_override" A2A_AGENT_URL || true)" == "http://gw.example/api/a2a/node-topology/" ]] || fail "a2a.agentUrl does not override A2A_AGENT_URL"
[[ "$(env_value "$a2a_dep" SERVICE_VERSION)" == "$app_version" ]] || fail "the a2a SERVICE_VERSION must default to the image tag / appVersion, like the api"
[[ "$(env_value "$a2a_dep" OTEL_SERVICE_NAME)" == "node-topology-a2a" ]] || fail "the a2a component must export as <otel.serviceName>-a2a"
[[ "$(env_value "$a2a_dep" OTEL_EXPORTER_OTLP_ENDPOINT)" == "$otel_endpoint" ]] || fail "the a2a component must export to otel.endpoint"
a2a_named="$(render_only a2a-deployment.yaml "${base[@]}" --set a2a.enabled=true --set otel.serviceName=topology)"
[[ "$(env_value "$a2a_named" OTEL_SERVICE_NAME)" == "topology-a2a" ]] || fail "the a2a service name must follow otel.serviceName"
[[ "$(env_value "$a2a_dep" ENVIRONMENT)" == "dev" ]] || fail "ENVIRONMENT must come from a2a.environment"
# The Service and the gateway route: the route rewrites /api/a2a/<context> to /
# (the JSON-RPC root, so /.well-known/agent-card.json stays reachable under the
# prefix) and needs BOTH flags.
a2a_svc="$(render_only a2a-service.yaml "${base[@]}" --set a2a.enabled=true)"
grep -q 'app.kubernetes.io/component: a2a' <<<"$a2a_svc" || fail "the a2a Service must pin component=a2a"
grep -q 'port: 8093' <<<"$a2a_svc" || fail "the a2a Service must expose 8093"
a2a_route_args=("${base[@]}" --set a2a.enabled=true --set a2a.route.enabled=true --set a2a.route.parentRefs[0].name=gw --set a2a.route.parentRefs[0].namespace=kong)
a2a_route="$(render_only a2a-route.yaml "${a2a_route_args[@]}")"
grep -q 'value: /api/a2a/node-topology$' <<<"$a2a_route" || fail "the a2a route must match /api/a2a/node-topology"
grep -q 'replacePrefixMatch: /$' <<<"$a2a_route" || fail "the a2a route must rewrite the prefix to /"
grep -q -- "- name: $release-a2a\$" <<<"$a2a_route" || fail "the a2a route must route to the a2a Service"
if grep -q 'kind: HTTPRoute' <<<"$(render_only a2a-route.yaml "${base[@]}" --set a2a.route.enabled=true 2>/dev/null || true)"; then
  fail "the a2a route must not render while a2a.enabled is false"
fi

# The web remote: its own nginx Deployment + ClusterIP Service, no DB/Kafka
# env, and no route in this chart (the web gateway owns /mfes/node-topology/).
frontend_dep="$(render_only frontend-deployment.yaml "${base[@]}" --set frontend.enabled=true)"
frontend_svc="$(render_only frontend-service.yaml "${base[@]}" --set frontend.enabled=true)"
grep -q 'app.kubernetes.io/component: frontend' <<<"$frontend_dep" || fail "the frontend Deployment was not rendered with frontend.enabled=true"
grep -q 'type: ClusterIP' <<<"$frontend_svc" || fail "the frontend Service must stay ClusterIP"
if grep -Eq 'name: (DATABASE_URL|KAFKA_BROKERS)$' <<<"$frontend_dep"; then fail "the frontend Deployment must not get database/Kafka env"; fi

# ALL optional components render TOGETHER (ct lint's all-components file).
together="$(render "${everything[@]}")"
for component in api mcp a2a frontend; do
  grep -q "app.kubernetes.io/component: $component\$" <<<"$together" || fail "the full render lacks the $component component"
done
if grep -E 'kind: (Ingress|HTTPRoute)' <<<"$together" | grep -q frontend; then
  fail "frontend routing must not live in this chart"
fi

echo "PASS: node-topology chart render assertions"
