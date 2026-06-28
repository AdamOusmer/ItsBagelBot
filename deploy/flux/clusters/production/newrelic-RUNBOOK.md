# New Relic nri-bundle: cutover to a Flux HelmRelease

Migrating the New Relic stack from its current **orphaned** state (installed via
`helm template | kubectl apply`, no helm release secret) to a Flux-managed
`HelmRelease` (`deploy/flux/clusters/production/newrelic.yaml`).

This runbook is **not executed automatically**. Do the steps in order. Steps are
read-only or single mutations; nothing here deletes monitoring data.

---

## 0. Ground truth (verified before writing this)

- `helm list -A` shows only tailscale/traefik. There is **no `sh.helm.release.v1.nri-bundle.*`
  secret** in ns `newrelic`, so helm has **zero ownership** of the live objects.
- Live objects carry `app.kubernetes.io/managed-by` / `helm.sh/chart` labels and
  a `kubectl.kubernetes.io/last-applied-configuration` annotation, i.e. they look
  helm-rendered but are owned by `kubectl`, not helm, not Flux.
- Chart pinned in the HelmRelease is **nri-bundle 6.0.36** (matches all six live
  subchart labels exactly).
- Live ENABLED components: newrelic-infrastructure, kube-state-metrics,
  newrelic-logging, newrelic-prometheus-agent. DISABLED (no workload, no webhook):
  nri-kube-events, nri-metadata-injection, k8s-agents-operator.
- All license/password secrets are already Doppler-synced (5 DopplerSecrets,
  also being adopted by Flux via `deploy/k8s-cluster/newrelic-dopplersecrets.yaml`).

---

## THE KEY RISK and the decision

When helm-controller installs a release, helm refuses to take over objects it did
not create unless they are explicitly marked as helm-owned:

```
Error: ... exists and cannot be imported into the current release:
invalid ownership metadata; ... missing key "app.kubernetes.io/managed-by":
must be set to "Helm" ... or label
"meta.helm.sh/release-name" must be "nri-bundle" ...
```

Three ways to resolve it:

| Option | What | Verdict |
|---|---|---|
| (a) **Pre-delete** orphaned objects, let helm recreate | `kubectl delete` the nri-bundle objects, then Flux installs fresh | **CHOSEN.** Simplest, deterministic, no half-owned state. Short monitoring gap (~1-2 min while pods restart), which is acceptable for an observability agent (it backfills/continues; no app impact). |
| (b) **Ownership takeover** (helm `--take-ownership`, Flux v2.4+ `driftDetection`/`force`) | Let helm/Flux adopt in place | Works, but our helm-controller adoption requires every object to be patched with `meta.helm.sh/release-name`, `meta.helm.sh/release-namespace`, `app.kubernetes.io/managed-by=Helm` first. That is option (c) by another name, on many objects, and is easy to get partially wrong. |
| (c) **Label/annotate for adoption** (patch every object with helm ownership meta, keep them in place, helm adopts on first apply) | Zero-gap, in-place adoption | Lowest risk to *data continuity* but HIGH risk of human error: you must patch **every** rendered object (Deployments, DaemonSets, StatefulSet, all ConfigMaps, ClusterRoles/Bindings, ServiceAccounts, Services, the admission objects) consistently. Miss one and the install still errors. |

**Decision: (a) pre-delete, with `--cascade=orphan` is NOT used** — we want the
pods gone and recreated by helm so the new valkey-discovery integration config
takes effect cleanly. The orphaned objects share names with what helm will
render, so a clean delete + helm install yields byte-identical names and the
Doppler-synced secrets (which we do NOT delete) are picked up immediately.

Rationale for accepting the brief gap: New Relic infra/logging/prometheus agents
are stateless collectors. A 1-2 minute pod restart drops at most a couple of
scrape intervals; there is no double-monitoring (old pods are gone before new
ones report) and no app-facing impact. Option (c)'s zero-gap benefit is not worth
its error surface on a hobby fleet.

---

## Pre-flight (read-only, do all of these first)

```sh
CTX=k8s-operator.tail451e6d.ts.net

# 1. Confirm there is genuinely no helm release to clash with.
helm --kube-context $CTX list -A | grep nri-bundle    # expect: no output
kubectl --context $CTX -n newrelic get secret | grep 'sh.helm.release'  # expect: none

# 2. Snapshot the live objects we are about to delete (rollback safety net).
kubectl --context $CTX -n newrelic get all,cm,secret,sa,role,rolebinding \
  -l 'app.kubernetes.io/managed-by' -o yaml > /tmp/nri-bundle-pre-cutover.yaml
kubectl --context $CTX get clusterrole,clusterrolebinding,mutatingwebhookconfiguration,validatingwebhookconfiguration \
  -o name | grep -i 'nri\|newrelic' > /tmp/nri-bundle-cluster-objs.txt

# 3. Confirm the 5 Doppler-synced secrets are healthy (we KEEP these).
kubectl --context $CTX -n newrelic get dopplersecret
kubectl --context $CTX -n newrelic get secret \
  nri-bundle-newrelic-infrastructure-license \
  nri-bundle-newrelic-logging-config \
  nri-bundle-newrelic-prometheus-agent-license \
  valkey-core-secret

# 4. Confirm valkey password key name (must be VALKEY_CORE_PASSWORD, NOT renamed).
kubectl --context $CTX -n newrelic get secret valkey-core-secret \
  -o jsonpath='{.data.VALKEY_CORE_PASSWORD}' | head -c 5   # expect non-empty base64
```

If any of pre-flight 1-4 is unexpected, STOP and re-investigate.

---

## Cutover

### Step 1 - Land the repo changes (PR -> main)

Merge the branch carrying:
- `deploy/flux/clusters/production/newrelic.yaml` (HelmRepository + HelmRelease)
- `deploy/k8s-cluster/newrelic-dopplersecrets.yaml` (5 DopplerSecrets adopted)
- deletion of `deploy/k8s-cluster/newrelic-values.yaml` (superseded partial overlay)
- `deploy/k8s-cluster/newrelic-logging.yaml` (unchanged; stays standalone, see "logging split")

**Do NOT let Flux reconcile the HelmRelease yet.** Suspend it the instant it
appears so the delete in Step 2 happens before helm-controller tries to install:

```sh
# As soon as the HelmRelease object exists (Flux created it from git):
flux --context $CTX suspend helmrelease nri-bundle -n newrelic
# The HelmRepository can reconcile freely; only the install must wait.
```

(If you prefer, push the HelmRelease with `spec.suspend: true` set, merge, confirm
Flux created it suspended, then continue. Either ordering is fine as long as the
release is suspended before Step 3.)

The DopplerSecret adoption (cluster Kustomization) is safe to reconcile now: it
only adds Flux ownership labels to the already-correct live DopplerSecrets; the
managed Secrets do not change.

### Step 2 - Delete the orphaned, kubectl-owned nri-bundle objects

Delete ONLY the helm-rendered workload/RBAC/config objects. **KEEP** the
Doppler-synced secrets and the namespace.

```sh
# Namespaced render objects (managed-by label present on all of them):
kubectl --context $CTX -n newrelic delete \
  deploy,ds,sts,svc,cm,sa,role,rolebinding \
  -l 'app.kubernetes.io/managed-by'

# The two logging ConfigMaps are Flux-owned (label kustomize.toolkit.fluxcd.io/name=cluster),
# NOT 'managed-by', so the selector above leaves them in place. Good - they are
# re-asserted by the cluster Kustomization and mounted by the chart. Leave them.

# Cluster-scoped render objects (review /tmp/nri-bundle-cluster-objs.txt first):
kubectl --context $CTX delete clusterrole,clusterrolebinding \
  -l 'app.kubernetes.io/managed-by' --ignore-not-found
# nri-metadata-injection mutating webhook: none live (verified). If one appears,
# delete it too - the new release keeps webhook disabled.

# DO NOT DELETE (Doppler-synced, reused by the new release):
#   secret/nri-bundle-newrelic-infrastructure-license
#   secret/nri-bundle-newrelic-logging-config
#   secret/nri-bundle-newrelic-prometheus-agent-license
#   secret/newrelic-key-secret
#   secret/valkey-core-secret
#   secret/doppler-token-secret
#   all dopplersecret/* objects
# DO NOT DELETE the leftover license secret nri-bundle-nri-kube-events-license
#   yet if you want a clean GC later; it is harmless (kube-events stays disabled).
```

Verify the namespace is now down to just secrets + dopplersecrets:

```sh
kubectl --context $CTX -n newrelic get all          # expect: no resources (or only terminating)
kubectl --context $CTX -n newrelic get secret        # expect: the Doppler-synced ones remain
```

### Step 3 - Resume Flux; let helm-controller install fresh

```sh
flux --context $CTX resume helmrelease nri-bundle -n newrelic
flux --context $CTX reconcile helmrelease nri-bundle -n newrelic --with-source
```

Because the conflicting objects are gone, helm installs cleanly and creates the
`sh.helm.release.v1.nri-bundle.v1` secret (this is what makes the stack
helm/Flux-owned going forward).

---

## Post-cutover verification

```sh
# 1. Release exists and is owned by Flux now.
helm --kube-context $CTX list -n newrelic                 # nri-bundle: deployed
flux --context $CTX get helmrelease -n newrelic           # Ready=True
kubectl --context $CTX -n newrelic get secret | grep sh.helm.release  # present

# 2. The four enabled components are back (and ONLY those four).
kubectl --context $CTX -n newrelic get deploy,ds,sts
#   expect: kube-state-metrics, nrk8s-ksm (Deploy); newrelic-logging,
#   nrk8s-controlplane, nrk8s-kubelet (DS); newrelic-prometheus-agent (STS).
#   expect: NO kube-events / metadata-injection / agents-operator workloads.

# 3. Valkey discovery is now LABEL-based, not a static 2-host list.
kubectl --context $CTX -n newrelic get cm nri-bundle-nrk8s-integrations-cfg \
  -o jsonpath='{.data.redis-config\.yml}'
#   expect: a `discovery:` block matching label.app.kubernetes.io/instance=valkey
#   and HOSTNAME=${discovery.ip}; NOT the old valkey-node-0/-1 FQDN list.

# 4. The agent spawns one nri-redis runner per valkey pod on each node.
kubectl --context $CTX -n newrelic logs ds/nri-bundle-nrk8s-kubelet -c agent \
  | grep -i 'nri-redis\|discovery'

# 5. In New Relic UI: all valkey instances (currently 4, plus any future
#    replicas) report as distinct entities. The static config only showed 2.

# 6. Logging + prometheus still flowing (license from Doppler secrets resolved).
kubectl --context $CTX -n newrelic logs ds/nri-bundle-newrelic-logging --tail=20
kubectl --context $CTX -n newrelic logs sts/nri-bundle-newrelic-prometheus-agent --tail=20
```

---

## Rollback

If the new install misbehaves:

```sh
flux --context $CTX suspend helmrelease nri-bundle -n newrelic
helm --kube-context $CTX uninstall nri-bundle -n newrelic   # if a release got created
kubectl --context $CTX -n newrelic apply -f /tmp/nri-bundle-pre-cutover.yaml
# Re-apply cluster-scoped objects from the snapshot too if they were deleted.
```

The Doppler-synced secrets were never deleted, so re-applying the snapshot
restores the previous (static-2-valkey) state.

---

## Logging split (why two ConfigMaps stay standalone)

`newrelic-logging` 1.33.1 has **no `fluentBit.luaScripts` and no
`existingConfigMap`** value. The custom pipeline needs `level_extract.lua` +
`payload.lua`, which cannot be expressed in chart values at this version. Both
logging ConfigMaps (`nri-bundle-newrelic-logging-fluent-bit-config` and
`nri-bundle-newrelic-logging-lua`) therefore remain the standalone, Flux-managed
manifests in `deploy/k8s-cluster/newrelic-logging.yaml` (already labelled
`kustomize.toolkit.fluxcd.io/name=cluster`). The chart's fluent-bit DaemonSet
mounts both by their fixed names, so the split is transparent to the pod.

There is a shared-name overlap on `...-fluent-bit-config`: helm renders one and
the cluster Kustomization re-asserts one of the same name. They render the same
content; last-writer wins and the mount is stable. To remove the overlap
entirely later, upgrade newrelic-logging to a version exposing
`fluentBit.config.existingConfigMap`, point it at the standalone CM, and drop the
in-values config. Tracked here so it is not lost.

---

## Recommendation: remove the inert annotation on the valkey StatefulSet

`deploy/k8s-valkey/statefulset.yaml` carries a `newrelic.com/integrations` pod
annotation (added during the static-config era). The kubelet `agent` container
(infrastructure-bundle 3.3.12) loads integrations from
`/etc/newrelic-infra/integrations.d/` only; it does **not** read pod annotations.
The annotation is therefore **inert** and misleading now that discovery is
label-based. **Recommend deleting it** from the valkey StatefulSet. That file is
owned by the valkey workstream, so this is a recommendation only - not changed
here.
```
