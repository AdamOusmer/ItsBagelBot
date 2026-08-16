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


def iter_manifests():
    for scan_dir in SCAN_DIRS:
        base = ROOT / scan_dir
        if not base.is_dir():
            continue
        for path in sorted(base.rglob("*.yml")) + sorted(base.rglob("*.yaml")):
            try:
                docs = list(yaml.safe_load_all(path.read_text()))
            except yaml.YAMLError as exc:
                yield path, None, f"YAML parse error: {exc}"
                continue
            for doc in docs:
                if isinstance(doc, dict) and doc.get("kind"):
                    yield path, doc, None


def check_ingressroute_services(path, name, namespace, services, failures):
    for svc in services or []:
        if not isinstance(svc, dict):
            continue
        if svc.get("kind") == "TraefikService":
            # Resolved separately when the TraefikService object itself is
            # walked below -- nothing to check on the reference.
            continue
        scheme = svc.get("scheme", "http")
        if scheme != "https":
            failures.append(
                f"{path}: IngressRoute {namespace}/{name} -> service "
                f"{svc.get('name')!r} port {svc.get('port')!r} scheme={scheme!r} "
                f"(must be scheme: https)"
            )


def check_doc(path, doc, failures):
    api_version = doc.get("apiVersion", "")
    kind = doc.get("kind", "")
    meta = doc.get("metadata", {}) or {}
    name = meta.get("name", "<unnamed>")
    namespace = meta.get("namespace", "<no-namespace>")

    if api_version == TRAEFIK_API and kind == "IngressRoute":
        spec = doc.get("spec", {}) or {}
        for route in spec.get("routes", []) or []:
            check_ingressroute_services(
                path, name, namespace, route.get("services"), failures
            )

    elif api_version == TRAEFIK_API and kind == "TraefikService":
        spec = doc.get("spec", {}) or {}
        weighted = spec.get("weighted") or {}
        check_ingressroute_services(
            path, name, namespace, weighted.get("services"), failures
        )
        mirroring = spec.get("mirroring")
        if mirroring:
            check_ingressroute_services(path, name, namespace, [mirroring], failures)
            check_ingressroute_services(
                path, name, namespace, mirroring.get("mirrors"), failures
            )

    elif api_version == TRAEFIK_API and kind in ("IngressRouteTCP", "IngressRouteUDP"):
        failures.append(
            f"{path}: {kind} {namespace}/{name} is not modeled by this checker "
            f"(no http-style backend scheme for TCP/UDP). Extend "
            f"check_route_schemes.py with an explicit rule for it before this "
            f"can merge -- failing closed instead of silently skipping it."
        )

    elif api_version == "networking.k8s.io/v1" and kind == "Ingress":
        spec = doc.get("spec", {}) or {}
        ingress_class = spec.get("ingressClassName") or ""
        if ingress_class not in ("", "traefik"):
            return  # different controller (e.g. tailscale); out of scope
        annotations = meta.get("annotations", {}) or {}
        if annotations.get(SERVERS_SCHEME_ANNOTATION) != "https":
            failures.append(
                f"{path}: Ingress {namespace}/{name} (class={ingress_class or 'traefik(default)'}) "
                f"missing '{SERVERS_SCHEME_ANNOTATION}: https' annotation"
            )


def main():
    failures = []
    parse_errors = []
    seen_any = False
    for path, doc, error in iter_manifests():
        seen_any = True
        if error:
            parse_errors.append(f"{path}: {error}")
            continue
        check_doc(path, doc, failures)

    if not seen_any:
        print("::error::no manifests found under deploy/ -- check is not running")
        return 1

    if parse_errors:
        print("YAML parse errors (failing closed):")
        for e in parse_errors:
            print(f"  {e}")

    if failures:
        print(f"{len(failures)} route object(s) with a non-https backend scheme:")
        for f in failures:
            print(f"  {f}")

    if failures or parse_errors:
        return 1

    print("all route objects under deploy/ use an https backend scheme.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
