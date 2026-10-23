#!/usr/bin/env python3
"""Assert this chart's Service selectors isolate its one component, and that
the api carries exactly the environment its binary reads.

Why the selector check exists (fleet-wide): `app.kubernetes.io/name` +
`instance` are identical on every pod a release creates. A Service that
selects on those two alone selects ALL components. Every Deployment here
carries `app.kubernetes.io/component` in selector.matchLabels and its pod
labels, and every Service selects on it, so each Service selects EXACTLY ONE
Deployment. This chart has one component: api (Deployment, Service, ConfigMap,
Secret, ServiceAccount, optional HPA / HTTPRoute / Ingress, all off by
default). The service consumes no topic, so there is no consumer group.

Run: python3 charts/node-topology/tests/test_service_selectors.py
Needs: helm, PyYAML.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

CHART_DIR = Path(__file__).resolve().parents[1]
RELEASE = "node-topology"

# Dummy values, no password: the chart refuses to render without a database
# source and without Kafka brokers.
BASE = [
    "--set", "database.url=postgres://u@example.invalid:5432/db",
    "--set", "kafka.brokers=kafka.example.invalid:9092",
]
ENABLE_EVERYTHING = BASE + [
    "--set", "autoscaling.api.enabled=true",
    "--set", "config.outboxRelayInterval=500ms",
    "--set", "config.shutdownDrainDelay=5s",
    "--set", "gatewayApi.enabled=true",
    "--set", "ingress.enabled=true",
]

# Every env var cmd/api reads.
API_ENV = (
    "HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL", "KAFKA_BROKERS",
    "OUTBOX_RELAY_INTERVAL", "OUTBOX_MAX_ATTEMPTS", "SHUTDOWN_DRAIN_DELAY",
)
# Never rendered: the service consumes no topic and reads no mode.
FORBIDDEN_SUFFIXES = ("_MODE", "_CONSUMER_GROUP")


def helm_template(extra_args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["helm", "template", RELEASE, str(CHART_DIR), *extra_args],
        capture_output=True, text=True,
    )


def render(extra_args: list[str]) -> list[dict]:
    result = helm_template(extra_args)
    if result.returncode != 0:
        raise SystemExit(f"FAIL: helm template {' '.join(extra_args)}:\n{result.stderr}")
    try:
        import yaml  # type: ignore
    except ModuleNotFoundError:  # pragma: no cover - environment guard
        print("SKIP: PyYAML not available; cannot assert selectors", file=sys.stderr)
        raise SystemExit(0)
    return [d for d in yaml.safe_load_all(result.stdout) if d]


def selector_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("selector") or {}


def pod_labels_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}


def matches(selector: dict, labels: dict) -> bool:
    return bool(selector) and all(labels.get(k) == v for k, v in selector.items())


def env_of(dep: dict) -> list[dict]:
    return dep["spec"]["template"]["spec"]["containers"][0].get("env", [])


def env_names(dep: dict) -> list[str]:
    return [e["name"] for e in env_of(dep)]


def env_value(dep: dict, name: str) -> str | None:
    return next((e.get("value") for e in env_of(dep) if e["name"] == name), None)


def check_selectors(docs: list[dict], failures: list[str]) -> None:
    services = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Service"}
    deployments = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Deployment"}

    if sorted(deployments) != [RELEASE]:
        failures.append(f"expected the single Deployment {[RELEASE]}, got {sorted(deployments)}")
    if sorted(services) != [RELEASE]:
        failures.append(f"expected the single Service {[RELEASE]}, got {sorted(services)}")
    svc = services.get(RELEASE)
    if svc is None or selector_of(svc).get("app.kubernetes.io/component") != "api":
        failures.append("the api Service selector must pin component=api")

    # Every Deployment's own selector must pin a component too, and be
    # satisfied by its pod labels.
    for name, dep in deployments.items():
        match_labels = dep["spec"]["selector"].get("matchLabels") or {}
        if "app.kubernetes.io/component" not in match_labels:
            failures.append(f"Deployment {name} selector.matchLabels lacks app.kubernetes.io/component")
        if not matches(match_labels, pod_labels_of(dep)):
            failures.append(f"Deployment {name} pod labels do not satisfy its own selector")

    # The real invariant: each Service selects exactly one Deployment.
    for svc_name, s in services.items():
        sel = selector_of(s)
        hit = [d for d, dep in deployments.items() if matches(sel, pod_labels_of(dep))]
        if len(hit) != 1:
            failures.append(f"Service {svc_name} selects {len(hit)} Deployments {sorted(hit)}; expected exactly 1")

    # The Service's named target port exists on every container it targets.
    for name, dep in deployments.items():
        ports = {p["name"] for p in dep["spec"]["template"]["spec"]["containers"][0].get("ports", [])}
        if "http" not in ports:
            failures.append(f"Deployment {name} does not expose the named port 'http'")

    api = deployments.get(RELEASE)
    if api is None:
        failures.append("the api Deployment was not rendered")
        return
    # The api HPA owns replicas when enabled.
    hpa = next((d for d in docs if d.get("kind") == "HorizontalPodAutoscaler"), None)
    if hpa is None or hpa["spec"]["scaleTargetRef"]["name"] != RELEASE:
        failures.append("the api HPA was not rendered (or does not scale the api Deployment)")
    elif "replicas" in api.get("spec", {}):
        failures.append("the api Deployment must omit replicas when its HPA owns them")
    # The HTTPRoute and the Ingress route to the api Service.
    route = next((d for d in docs if d.get("kind") == "HTTPRoute"), None)
    if route is None:
        failures.append("the HTTPRoute was not rendered with gatewayApi.enabled=true")
    else:
        refs = {b["name"] for r in route["spec"]["rules"] for b in r["backendRefs"]}
        if refs != {RELEASE}:
            failures.append(f"the HTTPRoute must route to the api Service only, got {sorted(refs)}")
    ingress = next((d for d in docs if d.get("kind") == "Ingress"), None)
    if ingress is None:
        failures.append("the Ingress was not rendered with ingress.enabled=true")
    else:
        backends = {p["backend"]["service"]["name"] for r in ingress["spec"]["rules"] for p in r["http"]["paths"]}
        if backends != {RELEASE}:
            failures.append(f"the Ingress must route to the api Service only, got {sorted(backends)}")

    check_env(api, failures)


def check_env(api: dict, failures: list[str]) -> None:
    api_env = env_names(api)
    for want in API_ENV:
        if want not in api_env:
            failures.append(f"the api Deployment does not render {want} from its dedicated value")
    for name in api_env:
        if name.endswith(FORBIDDEN_SUFFIXES):
            failures.append(f"the api Deployment renders {name}: the service has no mode and consumes no topic")
    duplicates = sorted({n for n in api_env if api_env.count(n) > 1})
    if duplicates:
        failures.append(f"the api Deployment renders env vars twice: {duplicates}")
    for name, want in (
        ("KAFKA_BROKERS", "kafka.example.invalid:9092"),
        ("OUTBOX_RELAY_INTERVAL", "500ms"),
        ("SHUTDOWN_DRAIN_DELAY", "5s"),
    ):
        if env_value(api, name) != want:
            failures.append(f"the api Deployment renders {name}={env_value(api, name)!r}, want {want!r}")
    for e in env_of(api):
        if e["name"] == "DATABASE_URL" and "secretKeyRef" not in e.get("valueFrom", {}):
            failures.append("DATABASE_URL must come from a secretKeyRef")


def check_defaults(failures: list[str]) -> None:
    """Default values deploy the api only: no HPA, route or ingress."""
    defaults = render(BASE)
    kinds = sorted(d["kind"] for d in defaults)
    stray = [k for k in kinds if k in {"HorizontalPodAutoscaler", "Ingress", "HTTPRoute"}]
    if stray:
        failures.append(f"optional resources rendered with default values: {stray}")
    dep = next(d for d in defaults if d.get("kind") == "Deployment")
    names = env_names(dep)
    # Optional values stay absent so the binary's own defaults apply.
    off = {"OUTBOX_RELAY_INTERVAL", "SHUTDOWN_DRAIN_DELAY"}
    if off & set(names):
        failures.append(f"these must be off with default values: {sorted(off & set(names))}")
    pod = dep["spec"]["template"]["spec"]
    if pod.get("terminationGracePeriodSeconds", 0) < 30:
        failures.append("terminationGracePeriodSeconds must be at least 30 by default")
    ctx = pod["containers"][0]["securityContext"]
    if not (ctx.get("readOnlyRootFilesystem") and ctx.get("allowPrivilegeEscalation") is False
            and ctx.get("capabilities", {}).get("drop") == ["ALL"]):
        failures.append("the api container must be hardened (read-only root, no escalation, drop ALL)")
    if not (pod["securityContext"].get("runAsNonRoot")
            and pod["securityContext"].get("seccompProfile", {}).get("type") == "RuntimeDefault"):
        failures.append("the api pod must run as non-root with the RuntimeDefault seccomp profile")


def check_refusals(failures: list[str]) -> None:
    for label, args, needle in (
        ("without a database source", ["--set", "kafka.brokers=k:9092"], "database.url or database.existingSecret"),
        ("without Kafka brokers", ["--set", "database.url=postgres://u@h/db"], "kafka.brokers"),
        ("a *_MODE variable through extraEnv",
         BASE + ["--set", "extraEnv[0].name=KAFKA_MODE", "--set", "extraEnv[0].value=permissive"], "KAFKA_MODE"),
        ("a consumer group through extraEnv",
         BASE + ["--set", "extraEnv[0].name=ORDERS_CONSUMER_GROUP", "--set", "extraEnv[0].value=g"], "consumes no topic"),
        ("a grace period under the shutdown worst case", BASE + ["--set", "terminationGracePeriodSeconds=20"], "terminationGracePeriodSeconds"),
    ):
        result = helm_template(args)
        if result.returncode == 0 or needle not in result.stderr:
            failures.append(f"chart rendered (or failed for another reason) {label}")


def main() -> int:
    failures: list[str] = []

    check_selectors(render(ENABLE_EVERYTHING), failures)
    check_defaults(failures)
    check_refusals(failures)

    if failures:
        for f in failures:
            print(f"FAIL: {f}")
        return 1

    print("PASS: the api Service selects exactly one Deployment (the chart has the api component only); "
          "HPA, HTTPRoute and Ingress are off by default; every cmd/api env var has a dedicated value, "
          "no *_MODE or *_CONSUMER_GROUP is rendered; the chart refuses to render without a database "
          "source, without brokers, with a short grace period, or with a *_MODE / consumer group in extraEnv")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
