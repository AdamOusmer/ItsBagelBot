# Falco: cutover to a Flux HelmRelease

Migrating falco from its current **orphaned** state (installed out-of-band, no
helm release secret in ns `falco`) to the Flux-managed `HelmRelease` in
`deploy/flux/clusters/production/falco.yaml`. Same class of migration as
`newrelic-RUNBOOK.md`; this one is much smaller (one DaemonSet, two ConfigMaps,
one Service, one ServiceAccount, plus dead falcosidekick leftovers).

This runbook is **not executed automatically**. Do the steps in order.

---

## 0. Ground truth (verified 2026-07-17)

- `helm list -n falco` is empty and there is **no `sh.helm.release.v1.falco.*`
  secret**, so helm owns nothing; the live objects are kubectl-owned orphans
  carrying `helm.sh/chart=falco-8.0.3` labels.
- Chart pinned in the HelmRelease is **falco 8.0.3** (app 0.43.1), matching
  every live label and both live images (falco:0.43.1, falcoctl:0.12.2).
- Values in falco.yaml reproduce the live install exactly: a
  `helm template --version 8.0.3` render with those values diffs clean against
  the live DaemonSet and both ConfigMaps except for (a) API-server-defaulted
  fields, (b) the new customRules ConfigMap/mount, (c) the dead
  `http_output.url` falcosidekick remnant we intentionally drop.
- The customRules file was validated inside a live falco pod against the
  live-loaded ruleset: `falco -V /etc/falco/falco_rules.yaml -V <file>` ->
  zero errors, zero warnings.
- Live orphan inventory in ns `falco`:
  - `daemonset/falco`, `configmap/falco`, `configmap/falco-falcoctl`,
    `service/falco-metrics`, `serviceaccount/falco` — all re-rendered by the
    new release (same names).
  - falcosidekick leftovers from an old experiment (sidekick is NOT enabled in
    the new release and these will NOT come back): `serviceaccount/falco-falcosidekick`,
    `serviceaccount/falco-falcosidekick-ui`, `role/falco-falcosidekick`,
    `role/falco-falcosidekick-ui`, `rolebinding/falco-falcosidekick`,
    `rolebinding/falco-falcosidekick-ui`.
  - `secret/newrelic-key-secret` (an NR license key for the dead sidekick
    output; nothing consumes it).
- **No cluster-scoped falco objects exist** (no ClusterRole/ClusterRoleBinding),
  and the new render creates none. With `driver.kind=modern_ebpf` the chart
  also skips the falco Role/RoleBinding and the driver-loader init container,
  so the rendered object set is exactly: SA, 3 ConfigMaps (falco,
  falco-falcoctl, falco-rules), Service falco-metrics, DaemonSet.

---

## THE KEY RISK and the decision

Identical to the nri-bundle cutover: helm refuses to take over objects it did
not create (`... exists and cannot be imported into the current release:
invalid ownership metadata ...`).

**Decision: pre-delete the orphans and let helm install fresh** (option (a)
from newrelic-RUNBOOK.md, for the same reasons). Falco is a detection-only
sensor; nothing at runtime depends on it. A 1-2 minute gap in syscall
monitoring while the DaemonSet is recreated is acceptable, and there is no
state to preserve (rules/plugins live in emptyDirs re-pulled by falcoctl at
pod start).

Failure mode if the ordering slips: if helm-controller tries to install while
the orphans still exist, the install just errors and the live orphaned falco
keeps running untouched. Delete the orphans and let the install remediation
(retries: 3) or a manual `flux reconcile` pick it up. Nothing breaks; you only
lose tidiness.

---

## Pre-flight (read-only)

```sh
CTX=k8s-operator.tail451e6d.ts.net

# 1. Still no helm release to clash with.
helm --kube-context $CTX list -n falco                       # expect: empty
kubectl --context $CTX -n falco get secret | grep sh.helm.release  # expect: none

# 2. Snapshot everything we are about to delete (rollback safety net).
kubectl --context $CTX -n falco get ds,svc,cm,sa,role,rolebinding -o yaml \
  > /tmp/falco-pre-cutover.yaml

# 3. Confirm there really are no cluster-scoped falco objects.
kubectl --context $CTX get clusterrole,clusterrolebinding -o name | grep -i falco
# expect: no output
```

---

## Cutover

### Step 1 - Land the repo change (merge to main)

Merge the branch carrying `deploy/flux/clusters/production/falco.yaml` and this
runbook. **Suspend the HelmRelease the moment Flux creates it**, so the delete
in Step 2 happens before helm-controller's first install attempt:

```sh
flux --context $CTX reconcile kustomization flux-system --with-source  # pull the merge
flux --context $CTX suspend helmrelease falco -n falco
# The falcosecurity HelmRepository can reconcile freely; only the install waits.
```

(If helm-controller wins the race anyway, see "Failure mode" above — no harm.)

### Step 2 - Delete the orphaned objects

```sh
# The release-rendered orphans (recreated by helm in Step 3):
kubectl --context $CTX -n falco delete \
  daemonset/falco configmap/falco configmap/falco-falcoctl \
  service/falco-metrics serviceaccount/falco

# The dead falcosidekick leftovers (NOT recreated; sidekick stays disabled):
kubectl --context $CTX -n falco delete \
  serviceaccount/falco-falcosidekick serviceaccount/falco-falcosidekick-ui \
  role/falco-falcosidekick role/falco-falcosidekick-ui \
  rolebinding/falco-falcosidekick rolebinding/falco-falcosidekick-ui

# KEEP: the namespace itself.
# OPTIONAL cleanup: secret/newrelic-key-secret holds an NR license key nothing
# consumes anymore (the key also lives in Doppler). Safe to delete; not
# required for the cutover.
```

### Step 3 - Resume Flux; let helm-controller install fresh

```sh
flux --context $CTX resume helmrelease falco -n falco
flux --context $CTX reconcile helmrelease falco -n falco --with-source
```

---

## Post-cutover verification

```sh
# 1. Release exists and is Flux-owned now.
helm --kube-context $CTX list -n falco                    # falco: deployed
flux --context $CTX get helmrelease -n falco              # Ready=True
kubectl --context $CTX -n falco get secret | grep sh.helm.release  # present

# 2. DaemonSet back to 4/4 (node1, node2, node3, worker1 — the worker-pool
#    toleration must keep worker1 covered).
kubectl --context $CTX -n falco get ds,pods -o wide

# 3. The exceptions file is mounted and LOADED (this is the whole point).
kubectl --context $CTX -n falco get cm falco-rules \
  -o jsonpath='{.data.bagelbot-exceptions\.yaml}' | head -5
kubectl --context $CTX -n falco logs ds/falco -c falco | grep -i 'rules.d\|Loading rules'
#   expect a "Loading rules from ... rules.d/bagelbot-exceptions.yaml" line and
#   NO rule-loading errors/warnings.

# 4. The three FP families go quiet. Immediately:
kubectl --context $CTX -n falco logs ds/falco -c falco --since=15m \
  | grep -c 'Contact K8S API Server'        # expect 0 (was hundreds/hour)
#    And over the next day in New Relic: falco volume drops from ~6.5k/day to
#    roughly zero (only real signal remains).

# 5. The sensor still detects (canary: read a sensitive file from a container
#    that is NOT allowlisted — the falco pod itself works and always exists;
#    the open() is the trigger, the content does not matter):
kubectl --context $CTX -n falco exec ds/falco -c falco -- cat /etc/shadow >/dev/null
kubectl --context $CTX -n falco logs ds/falco -c falco --since=2m | grep 'sensitive file'
#   expect one "Sensitive file opened for reading by non-trusted program" event.
```

---

## Rollback

```sh
flux --context $CTX suspend helmrelease falco -n falco
helm --kube-context $CTX uninstall falco -n falco    # if a release got created
kubectl --context $CTX -n falco apply -f /tmp/falco-pre-cutover.yaml
```

The snapshot restores the exact pre-cutover orphaned state (including the
sidekick leftovers). Revert the falco.yaml commit if abandoning entirely,
otherwise Flux will retry the install on the next reconcile.

---

## What changed vs the orphaned install (behavior)

1. **customRules** (`bagelbot-exceptions.yaml`, ConfigMap `falco-rules`,
   loaded from /etc/falco/rules.d): allowlists for the three stock-rule FP
   families (~6.5k events/day): systemd-userwork//etc/shadow +
   systemd-executor//etc/pam.d for "Read sensitive file untrusted";
   infra-namespace + nats-selfheal pod-prefix allowlist for "Contact K8S API
   Server From Container" (rule stays enabled); alpine/k8s acceptance-test
   pods (nats-live-acceptance / valkey-live-acceptance / wg-latency) for
   "Drop and execute new binary in container" and "Redirect STDOUT/STDIN to
   Network Connection in Container". Details and rationale live as comments in
   falco.yaml.
2. `falco.http_output.url` no longer points at the dead falco-falcosidekick
   Service (http_output was and stays disabled; the URL was inert).
3. The falcosidekick leftover SAs/Roles/RoleBindings are gone.
4. Everything else renders byte-identical to live (chart 8.0.3, modern_ebpf,
   json_output, metrics + falco-metrics Service, trimmed resources, infra-low
   priority class, worker-pool toleration, default falcoctl artifact config:
   falco-rules:5 + container plugin, follow every 168h).
