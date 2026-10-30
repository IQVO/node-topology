{{/*
Expand the name of the chart.
*/}}
{{- define "node-topology.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "node-topology.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "node-topology.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "node-topology.labels" -}}
helm.sh/chart: {{ include "node-topology.chart" . }}
{{ include "node-topology.selectorLabels" . }}
app.kubernetes.io/component: api
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels. The Deployment and the Service ALSO pin
app.kubernetes.io/component (api), so a Service never selects more than the
pods of one component (tests/test_service_selectors.py).
*/}}
{{- define "node-topology.selectorLabels" -}}
app.kubernetes.io/name: {{ include "node-topology.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "node-topology.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "node-topology.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL: the operator's own
(database.existingSecret) or the one this chart creates from database.url.
*/}}
{{- define "node-topology.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "node-topology.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Image reference. The release workflow tags the published image v<version> and
sets the chart appVersion to <version>, so an empty image.tag resolves to
v<appVersion>. An explicit image.tag is used verbatim.
*/}}
{{- define "node-topology.image" -}}
{{- $tag := .Values.image.tag | default (printf "v%s" (trimPrefix "v" (toString .Chart.AppVersion))) -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end }}

{{/*
MANDATORY: Postgres is the only runtime. The binary exits at boot without
DATABASE_URL, so a render without a database source is never a deployable
state: surface it at render time, naming the values.
*/}}
{{- define "node-topology.requireDatabase" -}}
{{- if not (or .Values.database.url .Values.database.existingSecret) -}}
{{- fail "node-topology requires database.url or database.existingSecret to be set (DATABASE_URL): the service runs on real Postgres only and exits at boot without it." -}}
{{- end -}}
{{- end -}}

{{/*
MANDATORY: Kafka is the only runtime. The binary exits at boot without
KAFKA_BROKERS; there is no default broker.
*/}}
{{- define "node-topology.requireKafka" -}}
{{- $_ := required "node-topology requires kafka.brokers to be set (KAFKA_BROKERS, comma-separated): the service runs on real Kafka only and exits at boot without it." .Values.kafka.brokers -}}
{{- end -}}

{{/*
Shutdown is sequential (cmd/api serveUntilSignal): SHUTDOWN_DRAIN_DELAY
(default 5s) + HTTP drain 10s + outbox relay 10s = 25s in the worst case at
the default delay. The delay is configurable, so the floor is computed from
config.shutdownDrainDelay (whole seconds, "5s"/"2m"/"7") plus 20s, and never
below 30s. A shorter grace period gets the pod SIGKILLed mid-drain.
*/}}
{{- define "node-topology.requireGracePeriod" -}}
{{- $drain := 5.0 -}}
{{- with .Values.config.shutdownDrainDelay -}}
{{- $d := toString . -}}
{{- if hasSuffix "ms" $d -}}
{{- $drain = div (int (trimSuffix "ms" $d)) 1000 | float64 -}}
{{- else if hasSuffix "m" $d -}}
{{- $drain = mulf (float64 (trimSuffix "m" $d)) 60.0 -}}
{{- else if hasSuffix "s" $d -}}
{{- $drain = float64 (trimSuffix "s" $d) -}}
{{- else -}}
{{- $drain = float64 $d -}}
{{- end -}}
{{- end -}}
{{- $want := int (ceil (addf $drain 20.0)) -}}
{{- if lt $want 30 }}{{- $want = 30 -}}{{- end -}}
{{- if lt (int .Values.terminationGracePeriodSeconds) $want -}}
{{- fail (printf "terminationGracePeriodSeconds is %v — want at least %d: SHUTDOWN_DRAIN_DELAY (5s default) + 10s HTTP drain + 10s outbox relay, never below 30." .Values.terminationGracePeriodSeconds $want) -}}
{{- end -}}
{{- end -}}

{{/*
This service consumes no topic, so there is no consumer group and no *_MODE
variable. Refuse a *_MODE (or a *_CONSUMER_GROUP) injected through extraEnv:
nothing in the binary reads one, and a stray one would suggest a runtime that
does not exist.
*/}}
{{- define "node-topology.requireNoModeEnv" -}}
{{- $owned := list "ENVIRONMENT" "SERVICE_VERSION" "OTEL_SERVICE_NAME" "OTEL_EXPORTER_OTLP_ENDPOINT" -}}
{{- range .Values.extraEnv -}}
{{- if has (toString .name) $owned -}}
{{- fail (printf "extraEnv sets %s — the chart renders it itself (otel.*, environment, image.tag); set the value instead." .name) -}}
{{- end -}}
{{- if hasSuffix "_MODE" (toString .name) -}}
{{- fail (printf "extraEnv sets %s — the *_MODE variables do not exist: the only runtime is Postgres + Kafka and the binary reads no mode." .name) -}}
{{- end -}}
{{- if hasSuffix "_CONSUMER_GROUP" (toString .name) -}}
{{- fail (printf "extraEnv sets %s — this service consumes no topic, so it has no consumer group." .name) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Fully qualified name of the optional MCP server workload (ADR 0008). Its own
Deployment + Service pair, pinned by app.kubernetes.io/component=mcp
(tests/test_service_selectors.py), so the api Service never selects it.
*/}}
{{- define "node-topology.mcpFullname" -}}
{{- include "node-topology.fullname" . }}-mcp
{{- end }}

{{/*
Fully qualified name of the A2A agent Deployment/Service (<release>-a2a,
docs/adr/0011). Both are pinned by app.kubernetes.io/component=a2a.
*/}}
{{- define "node-topology.a2aFullname" -}}
{{- include "node-topology.fullname" . }}-a2a
{{- end }}
