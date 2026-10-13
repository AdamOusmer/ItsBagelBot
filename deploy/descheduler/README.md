<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Descheduler

The scheduler places a pod once and never looks at it again. After a drain or a
reboot, the returning worker comes back empty while the other two carry its
pods, and nothing moves them back. The descheduler runs every 20 minutes as a
CronJob in `kube-system`, evicts a few pods that sit in the wrong place, and
lets the scheduler re-place them.

- `descheduler-v0.36.0.yaml` is upstream `kubernetes/base/rbac.yaml` plus
  `kubernetes/cronjob/cronjob.yaml` at v0.36.0, vendored verbatim except that
  the image carries the digest its tag resolved to. Do not edit.
- `kustomization.yaml` holds every local change: schedule, placement on
  `role=node`, `bagel-operator` priority, resources, job history.
- `policy.yaml` is the `DeschedulerPolicy`, mounted at `/policy-dir/policy.yaml`.

Descheduler minor versions track Kubernetes minors (v0.36 for k3s v1.36). Bump
it with the cluster, not ahead of it.

```bash
kubectl apply -k deploy/descheduler
kubectl -n kube-system get cronjob descheduler-cronjob
```

Use `-k`. `kubectl apply -f deploy/descheduler/` also reads
`kustomization.yaml` as a manifest and fails. The kustomization also drops the
upstream `Role` and `RoleBinding`, which only grant `kube-system` secrets read
that nothing here uses.

## Run it now, after a drain

Do not wait for the next tick once the node is uncordoned and Ready:

```bash
kubectl -n kube-system create job --from=cronjob/descheduler-cronjob descheduler-manual-$(date +%s)
kubectl -n kube-system logs -f job/<that name>
```

One run moves at most 2 pods per node and 2 per namespace. A fully emptied
worker takes a few runs to fill back up.

## Disable it

```bash
kubectl -n kube-system patch cronjob descheduler-cronjob -p '{"spec":{"suspend":true}}'
```

`suspend` is not in the manifest, so a later `kubectl apply -k` leaves it
suspended. Set it back to `false` to resume. `kubectl delete -k
deploy/descheduler` removes it entirely.

## What it evicts

Only pods in `app`, `db`, `cache` and `messaging`, selected by the
`kubernetes.io/metadata.name` label on `DefaultEvictor`, so a new namespace is
excluded until it is added there. Never evicted: pods with a PVC (valkey and
NATS, pinned to their node by `local-path`), pods with emptyDir or hostPath
volumes (gossip), DaemonSet pods, pods with no owner, and system-critical
pods. Every eviction goes through the Eviction API, so the PDBs
(`maxUnavailable: 1`) hold, and `nodeFit` skips a pod that no other worker
could take, which matters with required anti-affinity on 3 nodes.

Two strategies run:

- `RemovePodsViolatingTopologySpreadConstraint` with both `DoNotSchedule` and
  `ScheduleAnyway`. Our spreads are `ScheduleAnyway`, which upstream skips by
  default.
- `LowNodeUtilization` with thresholds 20/20/20 and targets 50/50/50 (cpu,
  memory, pods as % of allocatable, from requests, not live usage). It acts
  only when one worker is under 20% on all three and another is over 50% on
  any one. Measured 2026-10-10 at steady state: cpu 25/26/25%, memory
  54/54/56%, pods 33/30/32% on node1/2/3. No worker is under on all three, so
  it is a no-op; a worker that comes back empty is under on all three, and the
  others are over on memory, so it rebalances until the returning worker
  passes 20% on any resource.

Raise `thresholds` above the steady-state cpu (about 25%) and it starts moving
pods every run. Memory sits above the 50% target on every worker, so the
underutilized side is the only gate.

## Upgrading

Re-fetch the two upstream files at the new tag, re-resolve the image index
digest, keep `kustomization.yaml` and `policy.yaml`:

```bash
curl -sSL https://raw.githubusercontent.com/kubernetes-sigs/descheduler/vX.Y.Z/kubernetes/base/rbac.yaml
curl -sSL https://raw.githubusercontent.com/kubernetes-sigs/descheduler/vX.Y.Z/kubernetes/cronjob/cronjob.yaml
curl -sSL -o /dev/null -D - \
  -H 'Accept: application/vnd.docker.distribution.manifest.list.v2+json' \
  https://registry.k8s.io/v2/descheduler/descheduler/manifests/vX.Y.Z | grep -i docker-content-digest
```
