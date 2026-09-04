<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# KEDA

Event-driven autoscaling for `sesame` and `outgress`, which scale on JetStream
consumer lag rather than CPU. KEDA arrived as a Flux HelmRelease and left with
Flux (#588); this is the same thing as plain manifests.

- `keda-2.20.2.yaml` — the upstream release manifest, vendored verbatim except
  that the three images carry the digests their tags resolved to. Do not edit.
- `kustomization.yaml` — every local change: placement, priority, HA, resources.
- `availability.yaml` — one PodDisruptionBudget per component.

```bash
kubectl apply -f deploy/k8s/priorityclasses.yaml        # bagel-operator, referenced by the pods
kubectl apply -f deploy/messaging/network-policies.yaml # the scaler's NATS grant
kubectl apply --server-side --force-conflicts -k deploy/keda
kubectl -n keda rollout status deploy/keda-operator --timeout=5m
kubectl apply -f deploy/k8s/sesame.yaml   # the ScaledObjects, once the CRDs exist
kubectl apply -f deploy/k8s/outgress.yaml
```

`--server-side` is required, not a preference: the `scaledjobs.keda.sh` CRD is
larger than the 262144-byte annotation a client-side apply writes into
`last-applied-configuration`, so a plain `kubectl apply -k` fails on that one
object with `metadata.annotations: Too long` and leaves the rest applied.

To create only the autoscalers without rolling the services (their manifests
carry unrelated config), extract the `ScaledObject` documents from the two
service files and apply those alone.

`kubectl apply --dry-run=server -k deploy/keda` reports `namespaces "keda" not
found` for every namespaced object. That is the dry run, not the manifest: it
will not create the Namespace it is about to create. A real apply is ordered
Namespace first and succeeds.

## Things that will bite

**The NATS scaler needs pod-to-pod reach to port 8222.** It reads `/jsz` through
the `nats` Service, then follows the cluster to the consumer's leader and dials
that member's *advertised pod IP*. `deploy/messaging/network-policies.yaml`
denies pod-to-pod monitor traffic by design, so the `keda` namespace is listed
there explicitly. Apply that file before expecting a scaler to work.

**KEDA fails quiet.** A scaler that cannot reach NATS does not crash and does
not mark the ScaledObject unhealthy in any obvious way: it serves the static
`fallback` replicas (3 for both services) and the HPA looks fine. After any
change here, confirm it is really reading lag:

```bash
kubectl -n app get scaledobject,hpa
kubectl -n keda logs deploy/keda-operator | grep -i "nats\|error" | tail
```
A working scaler shows an external metric with a moving value; a broken one
sits at exactly the fallback replica count forever.

**A tailnet Service cannot front the monitor endpoint** (tried 2026-07-27): the
scaler ends up dialling `https://10.42.x.x/varz` and is refused. Tailnet names
inside pods also need a trailing dot, because `ndots:5` walks the search list
first and resolution fails outright.

**Placement.** KEDA runs on `role=node` (the three Intel workers): off cp1,
which is a tainted 2-core ARM node, and off worker1, the home node on weak
wifi. The old rule pinned KEDA to the control-plane node because the apiserver
calls its metrics adapter synchronously; since the 2026-08 rebuild
metrics-server has run unpinned on a worker with `v1beta1.metrics.k8s.io`
healthy, which is the same hop.

**Availability.** The adapter and the webhook run two replicas, spread one per
worker, with a PDB apiece: both are stateless and sit in a synchronous
apiserver path, where a single replica turns a node drain into failing HPA
reads and rejected `ScaledObject` writes.

The operator runs **one** replica, and that is deliberate. Upstream allows a
leader-elected standby, but the standby still answers the metrics adapter's
gRPC on `:9666` through the `keda-operator` Service without a populated scaler
cache: measured here on 2026-09-03, two replicas made external metric reads
time out about half the time (`FailedGetExternalMetric` on the HPA), and one
replica read 6/6 with `ScalingActive=ValidMetricFound`. If you scale it up,
expect scaling to become unreliable while every pod looks healthy.

**Priority.** `bagel-operator` (600000), below every first-party class: losing
KEDA degrades a control loop, not a request path — the services it scales keep
serving at their current replica count — so evicting it to seat a runtime
service is the right trade. It still outranks unclassified pods, because a
scaler that cannot schedule leaves the fleet frozen at its last decision.

**The NATS grant is scoped to the operator pod**, not the namespace: the two
selectors sit in one `from` element so they AND together. Only `keda-operator`
scrapes NATS — the adapter asks the operator over gRPC — so nothing else in
that namespace inherits a monitor grant.

**`spec.replicas` still lives in the Deployments.** Both services pin
`replicas: 3`, which equals `minReplicaCount`, so a `kubectl apply` while KEDA
is scaled up will pull the count back to 3 and KEDA will scale it out again on
its next poll (15s). Harmless, but do not read it as KEDA misbehaving.

## Upgrading

Re-download the release manifest, re-resolve the three digests, keep
`kustomization.yaml` as-is:

```bash
curl -LO https://github.com/kedacore/keda/releases/download/vX.Y.Z/keda-X.Y.Z.yaml
```
