{{/*
Fully qualified name of the frontend Module Federation remote deployment/service.

The remote (web/, `node_topology_mfe`) is served by its own nginx pod and
reached through the web gateway at /mfes/node-topology/. It is deliberately a
separate workload from the API: the gateway never routes to it, and the api
Service must never select it (component=frontend vs component=api). Kept in
its own helper file so the frontend component stays self-contained
(templates/frontend-*.yaml + this file + the frontend: values block).
*/}}
{{- define "node-topology.frontendFullname" -}}
{{- include "node-topology.fullname" . }}-frontend
{{- end }}
