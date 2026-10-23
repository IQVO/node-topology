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
# Every optional value set (ct lint's values file), so every env var the
# binary can read is rendered.
everything=(-f "$chart_dir/ci/default-values.yaml" \
  --set config.outboxRelayInterval=7s --set config.shutdownDrainDelay=3s)

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
  for f in "$repo_dir"/cmd/api/*.go "$repo_dir"/internal/adapters/outbound/postgres/config.go; do
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

# --- the chart is the api alone ---------------------------------------------------
for c in mcp analytics frontend projector reports; do
  if grep -q "app.kubernetes.io/component: $c\$" <<<"$all_docs"; then fail "unexpected $c component"; fi
done
grep -q 'app.kubernetes.io/component: api' <<<"$all_docs" || fail "the api component is missing"
if grep -qE 'kind: (HorizontalPodAutoscaler|Ingress|HTTPRoute)' <<<"$all_docs"; then
  fail "HPA, Ingress and HTTPRoute must be off by default"
fi

echo "PASS: node-topology chart render assertions"
