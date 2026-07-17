# Falco: Flux cutover (DONE 2026-07-17) + operating notes

Falco was installed out-of-band (no helm release secret in ns `falco`; the live
objects were kubectl-owned orphans carrying `helm.sh/chart=falco-8.0.3` labels).
It is now managed by the HelmRelease in
`deploy/flux/clusters/production/falco.yaml`.

**The cutover is complete.** This document records what actually happened
(because it contradicted the plan), the two findings worth keeping, and how to
roll back.

---

## What actually happened (and why the original plan was wrong)

The original plan, modelled on `newrelic-RUNBOOK.md`, was: suspend the
HelmRelease, **pre-delete the orphaned objects**, then resume so helm installs
fresh — on the premise that helm refuses to adopt objects it did not create
(`... exists and cannot be imported into the current release`).

**That premise does not hold for our helm-controller.** We run
**helm-controller v1.5.5**, which takes ownership of pre-existing objects by
default (Helm SDK 3.17+ `--take-ownership`; see `spec.install.disableTakeOwnership`).
When PR #438 merged, Flux reconciled and helm-controller **adopted every orphan
in place** before anyone could suspend anything:

- `helm install` succeeded on the first attempt (`falco/falco.v1`, chart 8.0.3).
- Every object gained `meta.helm.sh/release-name=falco`,
  `app.kubernetes.io/managed-by=Helm`, and the `helm.toolkit.fluxcd.io/*` labels.
- The DaemonSet rolled normally (4/4), **zero gap, nothing deleted**.

So: **no pre-delete was needed or performed.** If you are adopting another
orphaned release in this cluster, expect in-place adoption, not an error. The
newrelic runbook's option (a) is not the only path available to us.

Consequence: the **falcosidekick leftovers were never cleaned up**, because the
delete step never ran. They are inert (no falcosidekick workload exists; the
release does not render one) and safe to remove whenever:

```sh
CTX=k8s-operator.tail451e6d.ts.net
kubectl --context $CTX -n falco delete \
  serviceaccount/falco-falcosidekick serviceaccount/falco-falcosidekick-ui \
  role/falco-falcosidekick role/falco-falcosidekick-ui \
  rolebinding/falco-falcosidekick rolebinding/falco-falcosidekick-ui
# secret/newrelic-key-secret is an NR license key for that dead sidekick output;
# nothing consumes it (the key also lives in Doppler). Safe to delete too.
```

---

## FINDING: falco block-buffers stdout, and the FP noise was hiding it

**This is the important one. Do not remove `extra.args: ["-U"]` from
falco.yaml.**

falco 0.43.1 block-buffers its stdout alerts **even with
`buffered_outputs: false`** set (the chart documents that setting as "flushes
the output buffer on every alert" for `stdout_output`; measured 2026-07-17, it
does not). Alerts sit in the libc buffer until roughly 4KB accumulates.

This was invisible before, because the ~6,500 false-positive events/day acted as
an accidental **carrier**: the noise constantly filled and flushed the buffer,
so genuine alerts rode out with it within minutes. **Silencing the FPs removed
the carrier** — which meant a lone real alert would sit unflushed indefinitely
and never reach fluent-bit → New Relic. The noise fix alone would have quietly
turned falco into a detector whose alerts never leave the process.

Measured on node3 before the flag:

| Observation | Value |
|---|---|
| Two canary alerts (11:15:54, 11:18:47) visible in `kubectl logs` | **No — for 11+ minutes** |
| Same alerts after a 60-event burst filled the buffer | Appeared, retroactively |
| Single alert, 30s wait | Never emitted |
| Engine `rules_matches_total` vs lines actually emitted | **63 vs 61** |

The fix is `-U`/`--unbuffered` via the chart's `extra.args`, which forces a
flush per alert. Cost is slightly higher CPU per alert, which is irrelevant at
our (now near-zero) alert rate.

**Diagnostic worth reusing:** `rules_matches_total` counts what the *engine*
matched, independent of whether anything was ever *emitted*. Comparing it against
emitted lines is what exposed this, and it is the fastest way to tell "falco saw
nothing" apart from "falco saw it and swallowed it":

```sh
kubectl --context $CTX -n falco exec ds/falco -c falco -- \
  curl -s http://localhost:8765/metrics | grep 'rules_matches_total{'
kubectl --context $CTX -n falco logs ds/falco -c falco | grep -c '"rule"'
```

---

## Verification (all passed 2026-07-17)

```sh
CTX=k8s-operator.tail451e6d.ts.net

# 1. Release is Flux-owned.
helm --kube-context $CTX list -n falco          # falco: deployed, chart falco-8.0.3
flux --context $CTX get helmrelease -n falco    # Ready=True

# 2. DaemonSet 4/4 (the worker-pool toleration must keep worker1 covered).
kubectl --context $CTX -n falco get ds,pods -o wide

# 3. Both rule files load on EVERY node, schema ok, no warnings.
kubectl --context $CTX -n falco logs ds/falco -c falco | grep -A3 'Loading rules from'
#   expect: /etc/falco/falco_rules.yaml AND
#           /etc/falco/rules.d/bagelbot-exceptions.yaml, both "schema validation: ok"

# 4. The FP families are silent: rules_matches_total shows NO matches for
#    "Contact K8S API Server From Container" / "Read sensitive file untrusted"
#    / drop-and-execute / stdio-redirect across all 4 pods.

# 5. The sensor still DETECTS (canary from a non-allowlisted path). With -U the
#    alert must appear within seconds; before -U it would not appear at all.
kubectl --context $CTX -n falco exec ds/falco -c falco -- cat /etc/shadow >/dev/null
sleep 5
kubectl --context $CTX -n falco logs ds/falco -c falco --since=1m | grep 'sensitive file'
#   expect exactly one "Sensitive file opened for reading by non-trusted program".
#   proc.exepath is cat/busybox, NOT systemd-*, so the allowlist correctly
#   does not cover it. This canary is the proof that the exceptions tuned the
#   sensor rather than deafening it — re-run it after ANY change to the rules.
```

Note on libbpf noise: node1 (ARM/aarch64) logs
`failed to create tracepoint 'syscalls/sys_enter_creat'/'sys_enter_open'`.
Expected — arm64 has no `creat`/`open` syscalls, only `openat`. Pre-existing,
not caused by the cutover, and harmless.

---

## Rollback

```sh
flux --context $CTX suspend helmrelease falco -n falco
helm --kube-context $CTX uninstall falco -n falco
```

**Read this before uninstalling:** because helm *adopted* the pre-existing
objects rather than creating them, `helm uninstall` will **delete the live
falco** (DaemonSet, ConfigMaps, Service, SA) — it does not "give them back".
There is no pre-cutover snapshot to restore from, since nothing was deleted on
the way in. To restore, re-apply the chart at 8.0.3 (values in falco.yaml) or
revert the git commit and let Flux reinstall. Falco is a detection-only sensor —
nothing at runtime depends on it, and rules/plugins live in emptyDirs that
falcoctl re-pulls at pod start — so a gap costs visibility, not availability.

---

## What this release changes vs the original orphaned install

1. **customRules** (`bagelbot-exceptions.yaml`, ConfigMap `falco-rules`, mounted
   at /etc/falco/rules.d): allowlists for the three stock-rule FP families
   (~6.5k events/day). Rationale per family lives in falco.yaml's comments.
2. **`-U`/unbuffered stdout** — see the FINDING above. Ships *with* the noise
   fix, not after it.
3. `falco.http_output.url` no longer points at the dead `falco-falcosidekick`
   Service (`http_output` was and stays disabled; the URL was inert).
4. Everything else renders identical to the pre-cutover live install: chart
   8.0.3, modern_ebpf, json_output, metrics + falco-metrics Service, trimmed
   resources, infra-low priority class, worker-pool toleration, and the default
   falcoctl artifact config (falco-rules:5 + container plugin, follow 168h).
