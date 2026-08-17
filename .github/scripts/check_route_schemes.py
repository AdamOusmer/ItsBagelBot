#!/usr/bin/env python3
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.
#
# Staged target path: .github/scripts/check_route_schemes.py
#
# Enforces "traefik -> backend is always https, no exceptions" at the object
# level. Traefik 3.6 has no global default-backend-scheme switch (confirmed
# against the deployed rancher/mirrored-library-traefik:3.6.13 / chart
# traefik-39.0.701_up39.0.7 -- scheme is a per-service field), so this has to
# walk every route object instead of flipping one setting.
#
# Covered:
#   - traefik.io/v1alpha1 IngressRoute:  every entry in spec.routes[].services[]
#   - traefik.io/v1alpha1 TraefikService: spec.weighted.services[] and
#     spec.mirroring (+ .mirrors[]) -- these are just other places an
#     IngressRoute's `services[].kind: TraefikService` can hide a backend.
#   - networking.k8s.io/v1 Ingress, only when it routes through Traefik
#     (ingressClassName unset/"" or "traefik" -- the cluster's default
#     class). Requires the explicit
#     traefik.ingress.kubernetes.io/service.serversscheme: https annotation.
#     Ingress objects on another class (e.g. ingressClassName: tailscale) are
#     skipped: a different controller entirely, with its own TLS model.
#
# Deliberately fails closed, not skips, on:
#   - traefik.io/v1alpha1 IngressRouteTCP / IngressRouteUDP: TCP does not
#     have an http-style backend "scheme" (it's passthrough vs TLS
#     termination via spec.tls), so this script does not attempt to judge
#     it -- but silently ignoring a new TCP route would be exactly the kind
#     of gap "no exceptions" is supposed to catch. None exist in the repo
#     today; adding one must fail CI until a human extends this checker.
#   - Any YAML parse error under the scanned paths.
#
# Known, accepted gap (documented, not hidden): Traefik's Kubernetes Ingress
# provider auto-detects scheme=https when a Service's port is 443 or named
# "https", even without the annotation. This checker does not special-case
# that and always demands the explicit annotation -- stricter than the
# minimum Traefik requires, never a false negative, so it cannot let a
# plaintext backend through by relying on that implicit behavior.
import pathlib
import sys

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[2]
SCAN_DIRS = ["deploy"]

TRAEFIK_API = "traefik.io/v1alpha1"
SERVERS_SCHEME_ANNOTATION = "traefik.ingress.kubernetes.io/service.serversscheme"


def iter_manifest_paths():
    """Every YAML file under the scanned directories, in stable order."""
    for scan_dir in SCAN_DIRS:
        base = ROOT / scan_dir
        if not base.is_dir():
            continue
        yield from sorted(base.rglob("*.yml"))
        yield from sorted(base.rglob("*.yaml"))


def iter_docs_in(path):
    """Yield (doc, error) for one file. A parse failure yields a single error."""
    try:
        docs = list(yaml.safe_load_all(path.read_text()))
    except yaml.YAMLError as exc:
        yield None, f"YAML parse error: {exc}"
        return
    for doc in docs:
        if isinstance(doc, dict) and doc.get("kind"):
            yield doc, None


def iter_manifests():
    for path in iter_manifest_paths():
        for doc, error in iter_docs_in(path):
            yield path, doc, error


# Routes whose backend genuinely cannot serve TLS, keyed by
# (namespace, IngressRoute name, backend service name).
#
# This is not a place to silence inconvenient findings. An entry belongs here
# only when the backend is a third-party binary with no TLS support at all, so
# no amount of configuration on our side can satisfy the check. Every entry
# carries its justification and is printed on each run, so it stays visible
# rather than quietly accumulating. Anything not listed here still fails.
UPSTREAM_TLS_INCAPABLE = {
    ("flux-system", "webhook-receiver", "webhook-receiver"): (
        "notification-controller (flux v2.8.8 / controller v1.8.4) exposes no "
        "TLS flags on --receiverAddr or anywhere else, so the receiver cannot "
        "serve HTTPS. Closing this needs a TLS-terminating sidecar patched into "
        "a Flux bootstrap-managed Deployment. Remove this entry the moment "
        "upstream gains TLS support or that sidecar lands."
    ),
}


class Report:
    """Collects what the walk found, so handlers take one object, not two lists."""

    def __init__(self):
        self.failures = []
        self.exempted = []


class Doc:
    """A manifest document plus where it came from."""

    def __init__(self, path, doc):
        self.path = path
        self.doc = doc
        self.meta = doc.get("metadata", {}) or {}
        self.spec = doc.get("spec", {}) or {}
        self.kind = doc.get("kind", "")
        self.name = self.meta.get("name", "<unnamed>")
        self.namespace = self.meta.get("namespace", "<no-namespace>")


def check_service(ctx, svc, report):
    if not isinstance(svc, dict) or svc.get("kind") == "TraefikService":
        # A TraefikService reference is resolved when that object is walked
        # itself -- nothing to check on the reference.
        return
    if svc.get("scheme", "http") == "https":
        return
    reason = UPSTREAM_TLS_INCAPABLE.get((ctx.namespace, ctx.name, svc.get("name")))
    if reason is not None:
        report.exempted.append(
            f"{ctx.namespace}/{ctx.name} -> {svc.get('name')!r}: {reason}"
        )
        return
    report.failures.append(
        f"{ctx.path}: {ctx.kind} {ctx.namespace}/{ctx.name} -> service "
        f"{svc.get('name')!r} port {svc.get('port')!r} "
        f"scheme={svc.get('scheme', 'http')!r} (must be scheme: https)"
    )


def check_services(ctx, services, report):
    for svc in services or []:
        check_service(ctx, svc, report)


def check_ingressroute(ctx, report):
    for route in ctx.spec.get("routes") or []:
        check_services(ctx, route.get("services"), report)


def check_traefikservice(ctx, report):
    check_services(ctx, (ctx.spec.get("weighted") or {}).get("services"), report)
    mirroring = ctx.spec.get("mirroring")
    if not mirroring:
        return
    check_services(ctx, [mirroring], report)
    check_services(ctx, mirroring.get("mirrors"), report)


def check_stream_route(ctx, report):
    # TCP/UDP routes have no http-style backend scheme, so this checker cannot
    # judge them. Fail closed rather than skip: a new one must be modelled
    # deliberately before it can merge.
    report.failures.append(
        f"{ctx.path}: {ctx.kind} {ctx.namespace}/{ctx.name} is not modeled by "
        f"this checker (no http-style backend scheme for TCP/UDP). Extend "
        f"check_route_schemes.py with an explicit rule for it before this "
        f"can merge -- failing closed instead of silently skipping it."
    )


def check_ingress(ctx, report):
    ingress_class = ctx.spec.get("ingressClassName") or ""
    if ingress_class not in ("", "traefik"):
        return  # different controller (e.g. tailscale); out of scope
    annotations = ctx.meta.get("annotations", {}) or {}
    if annotations.get(SERVERS_SCHEME_ANNOTATION) == "https":
        return
    report.failures.append(
        f"{ctx.path}: Ingress {ctx.namespace}/{ctx.name} "
        f"(class={ingress_class or 'traefik(default)'}) "
        f"missing '{SERVERS_SCHEME_ANNOTATION}: https' annotation"
    )


# Dispatch on (apiVersion, kind). Adding a route type means adding a handler
# here, not extending a conditional chain.
HANDLERS = {
    (TRAEFIK_API, "IngressRoute"): check_ingressroute,
    (TRAEFIK_API, "TraefikService"): check_traefikservice,
    (TRAEFIK_API, "IngressRouteTCP"): check_stream_route,
    (TRAEFIK_API, "IngressRouteUDP"): check_stream_route,
    ("networking.k8s.io/v1", "Ingress"): check_ingress,
}


def check_doc(path, doc, report):
    handler = HANDLERS.get((doc.get("apiVersion", ""), doc.get("kind", "")))
    if handler is None:
        return
    handler(Doc(path, doc), report)


def walk(report):
    """Walk every manifest, returning parse errors and whether anything was seen."""
    parse_errors = []
    seen_any = False
    for path, doc, error in iter_manifests():
        seen_any = True
        if error:
            parse_errors.append(f"{path}: {error}")
            continue
        check_doc(path, doc, report)
    return parse_errors, seen_any


def print_block(heading, lines):
    if not lines:
        return
    print(heading.format(n=len(lines)))
    for line in lines:
        print(f"  {line}")


def main():
    report = Report()
    parse_errors, seen_any = walk(report)

    if not seen_any:
        print("::error::no manifests found under deploy/ -- check is not running")
        return 1

    print_block("YAML parse errors (failing closed):", parse_errors)
    print_block(
        "{n} route(s) exempted -- backend cannot serve TLS upstream:",
        report.exempted,
    )
    print_block(
        "{n} route object(s) with a non-https backend scheme:",
        report.failures,
    )

    if report.failures or parse_errors:
        return 1

    print("all route objects under deploy/ use an https backend scheme.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
