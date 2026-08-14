# Runbook: gateway → gossip cutover

One-time operator procedure for landing the `gateway` → `gossip` rename.

The rename is not just a code change. It renames a NATS account, its user, and
its bcrypt credential, and it renames the Doppler project the workload reads.
Applying the manifests before the credentials exist takes down the **entire NATS
broker**, not just this one service — `nats-server` refuses to start on a config
containing an unresolved `$VAR`, and every fleet service depends on that broker.

Read the whole document before running anything.

## What changes

| | Before | After |
|---|---|---|
| Go package | `app/gateway` | `app/gossip` |
| RPC subjects | `bagel.rpc.gateway.>` | `bagel.rpc.gossip.>` |
| Health subject | `bagel.rpc.health.gateway` | `bagel.rpc.health.gossip` |
| NATS account | `GATEWAY_RPC` | `GOSSIP_RPC` |
| NATS user | `gateway_rpc` | `gossip_rpc` |
| Broker credential | `$NATS_BCRYPT_GATEWAY_RPC` | `$NATS_BCRYPT_GOSSIP_RPC` |
| Doppler project | `gateway` | `gossip` |
| Workload token Secret | `gateway-doppler-token` | `gossip-doppler-token` |
| k8s Deployment / manifest | `gateway` / `gateway.yaml` | `gossip` / `gossip.yaml` |
| Subject prefix env | `NATS_GATEWAY_SUBJECT_PREFIX` | `NATS_GOSSIP_SUBJECT_PREFIX` |

The subject prefix is a **hard cutover** — the code no longer falls back to
`NATS_GATEWAY_SUBJECT_PREFIX`. A stale value left in Doppler would resolve to
old-prefix subjects against new-prefix ACLs, which NATS denies silently.

## The ordering constraint

The bcrypt hashes reach the broker as **environment variables** (`nats-auth-env`,
injected via `envFrom`), while `nats-auth.conf` reaches it as a **ConfigMap** that
hot-reloads via the config-reloader sidecar. Environment variables do not
hot-reload; the pod must restart to see a new one.

So the new credential has to be present in the broker's environment *before* the
config that references it is applied:

```
add NATS_BCRYPT_GOSSIP_RPC  →  restart nats pods  →  apply renamed conf
```

Reverse those and the broker fails to start.

Keep `NATS_BCRYPT_GATEWAY_RPC` in place throughout. During the overlap the broker
carries both variables; the old conf references only the old one, the new conf
references only the new one, and an unreferenced extra variable is harmless. That
overlap is what makes the cutover safe and reversible.

## Prerequisites

Both are created out-of-band and are **not** in git.

1. **Doppler project `gossip`, config `prd`**, inheriting `production-full` to match
   `deploy/doppler/topology.json`. It must carry everything the old `gateway`
   project had: `VALKEY_*`, `URCHIN_API_KEY`, optional `MCSR_API_KEY`,
   `NEW_RELIC_*`, `FORTNITE_*`.
2. **Secret `gossip-doppler-token`** in namespace `production`, holding a Doppler
   service token scoped to `gossip/prd`. `deploy/k8s/gossip.yaml` names it in
   `spec.tokenSecret`; without it the DopplerSecret never syncs, `gossip-env` is
   never created, and the Deployment stays unschedulable.

Verify before proceeding:

```bash
doppler secrets -p gossip -c prd --only-names
kubectl -n production get secret gossip-doppler-token
```

## A caution on nats-secrets.py

`deploy/k8s/nats-secrets.py` loops over every entry in `SERVICES` and mints a
**new password for each one**. It is a full-fleet credential rotation, not a
targeted one. Running it here would force every service in the fleet to restart
to pick up its new credential, turning a single-service rename into a fleet-wide
rotation.

For this cutover, set only the two values the rename needs. Use the script's own
hashing so the format matches exactly (cost 11, `$2a` prefix):

```bash
python3 - <<'PY'
import secrets, bcrypt
pw = secrets.token_hex(24)
print("NATS_RPC_PASSWORD =", pw)
print("NATS_BCRYPT_GOSSIP_RPC =", bcrypt.hashpw(pw.encode(), bcrypt.gensalt(11, prefix=b"2a")).decode())
PY
```

Then write the plaintext to the workload project and the hash to the broker project:

```bash
doppler secrets set -p gossip -c prd NATS_RPC_USER=gossip_rpc NATS_RPC_PASSWORD=<pw>
doppler secrets set -p nats   -c prd NATS_BCRYPT_GOSSIP_RPC=<hash>
```

The rename of the `SERVICES` key in `nats-secrets.py` is still correct and still
required — it keeps future rotations emitting the right names. It just should not
be the tool used to perform this cutover.

## Procedure

### 1. Seed the new credential

Run the prerequisite checks, then the two `doppler secrets set` commands above.
Leave `NATS_BCRYPT_GATEWAY_RPC` alone.

### 2. Let the operator resync

The DopplerSecret resyncs on a 60s interval. Confirm the broker secret carries
both keys before continuing:

```bash
kubectl -n production get secret nats-auth-env -o jsonpath='{.data}' \
  | tr ',' '\n' | grep -E 'GATEWAY_RPC|GOSSIP_RPC'
```

Both must appear. Do not proceed on only one.

### 3. Restart the broker to pick up the environment

```bash
kubectl -n production rollout restart statefulset nats
kubectl -n production rollout status  statefulset nats
kubectl -n production rollout restart daemonset nats-leaf
kubectl -n production rollout status  daemonset nats-leaf
```

The broker is still running the **old** config here, which references only
`$NATS_BCRYPT_GATEWAY_RPC`. Nothing should change behaviorally. If a pod
crash-loops, stop and read its logs — an unresolved `$VAR` names itself plainly.

### 4. Merge the rename

Merging `chore/gossip` lets Flux apply the renamed `nats-auth.conf` (via the
`configMapGenerator`), the renamed Deployment, and the corrected NetworkPolicy
selectors. The config-reloader SIGHUPs the broker, which resolves
`$NATS_BCRYPT_GOSSIP_RPC` from the environment already loaded in step 3.

Flux prunes on `./deploy/k8s`, so the old `gateway` Deployment is garbage
collected once `gateway.yaml` is gone from the tree. No manual delete.

### 5. Verify

```bash
kubectl -n production rollout status deployment gossip
kubectl -n production get pods -l app=gossip
kubectl -n production logs -l app=gossip --tail=50
kubectl -n production get pods -l app=gateway          # expect: no resources found
```

Then confirm the RPC path end to end — a `!fn`, `!mcsr`, or `!store` command in a
live channel exercises sesame → gossip → upstream. Check the admin console's
health tile for `gossip`.

Confirm the NetworkPolicies actually select the new pods; this is the step most
likely to pass silently while being wrong:

```bash
kubectl -n production describe networkpolicy default-deny-apps | grep -A3 'Pod Selector'
```

`gossip` must appear in the `app In (...)` set. If it does not, the pods are
outside every policy and therefore unrestricted in both directions.

### 6. Clean up, after an observation window

Only once step 5 is fully green:

```bash
doppler secrets delete -p nats -c prd NATS_BCRYPT_GATEWAY_RPC
doppler secrets delete -p gossip -c prd NATS_GATEWAY_SUBJECT_PREFIX   # if it was copied over
```

Leaving `NATS_GATEWAY_SUBJECT_PREFIX` set is harmless with the current code, which
ignores it — but delete it so a future reader is not misled into thinking it is
still wired up.

Archive the `gateway` Doppler project and delete the `gateway-doppler-token`
Secret last, per the retention note in `deploy/doppler/README.md`.

## Rollback

Before step 4, rollback is free: nothing has changed except an unused extra
environment variable.

After step 4, revert the merge. Flux reapplies the previous `nats-auth.conf`,
which references `$NATS_BCRYPT_GATEWAY_RPC` — still present in the broker
environment as long as step 6 has not run. This is exactly why step 6 waits for an
observation window; running it early removes the rollback path.

If the broker is already down on an unresolved variable, restore the missing key
in the `nats` Doppler project, wait for the resync, and restart the nats pods.
The config file alone cannot fix it: the value arrives through the environment.
