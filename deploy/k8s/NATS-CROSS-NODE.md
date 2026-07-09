# NATS cross-node interest — diagnosis & remediation

Operator runbook for the one traffic class the fleet has that genuinely needs
**cross-node, non-local, non-queue** delivery on a `*_RPC` account: the outgress
lease **permit borrow** (`bagel.outgress.permit.v2.<region>.<podID>`, a plain sub
per [borrow.go](../../pkg/ratelimit/borrow.go)).

Every other RPC in the fleet is a **queue** group with an instance on every node,
so the leaf mesh answers it on-node (zero hops) and its cross-node path is never
exercised. Borrow is the first path that must reach a *specific remote* pod, so it
is the first to expose any cross-node interest-propagation gap. The old lease
membership discovery (`$SRV.INFO` scatter-gather) hit the same wall and reported
`members: 1`; that half is now fixed by moving membership to Valkey
([lease_client.go](../../pkg/ratelimit/lease_client.go)), so **correctness no
longer depends on this** — a lone pod divides quota by member count and the fleet
stays ≤100%. Borrow is pure burst efficiency and fails safe to the shared
emergency partition. Fix this to *recover the burst headroom*, not to stop 429s.

## The config is already correct — do not blind-edit it

Confirmed wiring (leave as-is unless a step below proves otherwise):

- Leaf cluster: `cluster{ name: nats-leaf, port: 6222, routes: nats-leaf-peers:6222, no_advertise }` ([nats-leaf-server.conf](nats-leaf-server.conf)); `nats-leaf-peers` is headless + `publishNotReadyAddresses` ([nats-leaf.yaml](nats-leaf.yaml)).
- Hub cluster: 3-replica StatefulSet, routes on `nats-headless:6222`; per-account leafnode listener on `7422` ([nats.yaml](nats.yaml), [nats-server.conf](nats-server.conf)).
- `OUTGRESS_RPC` has no per-user subject ACL (default-allow **within** the account), so `bagel.outgress.permit.v2.>` and `$SRV.>` are unrestricted account-internal subjects.
- `network-policies.yaml` `default-deny-apps` selects only app pods, **not** `nats`/`nats-leaf`, so NATS pods are NetworkPolicy-unrestricted. `linkerd-auth.yaml` defines no `Server` for NATS, so its ports are not locked to default-deny. `opaque-ports` covers `4222,6222,7422`.

So the break is **runtime**, on one of: leaf↔leaf routes (6222), leaf→hub leafnode (7422), or hub↔hub interest relay.

## Diagnose (live cluster)

Run from the operator context (`k8s-operator.tail451e6d.ts.net`).

1. **Leaf mesh formed?** Each leaf should hold a route to every *other* leaf.
   ```sh
   for p in $(kubectl -n production get pod -l app=nats-leaf -o name); do
     echo "$p:"; kubectl -n production exec "$p" -c nats -- \
       wget -qO- localhost:8222/routez | grep -E '"num_routes"'
   done   # expect num_routes == (leaf count - 1) on every leaf
   ```
   Prometheus equivalent: `gnatsd_varz_routes{app="nats-leaf"}`.

2. **Leafnode links up?** Each leaf opens one remote per account (13).
   ```sh
   kubectl -n production exec <a-leaf-pod> -c nats -- \
     wget -qO- localhost:8222/leafz | grep -E '"leafnodes"|OUTGRESS'
   ```

3. **Does interest actually cross?** Sub on one node's leaf, pub from another's,
   with the OUTGRESS_RPC (`outgress_rpc`) creds:
   ```sh
   # terminal A — on the leaf co-located with outgress pod B's node
   nats --server nats://nats-leaf.<nodeB>:4222 --user outgress_rpc --password $PW \
     sub 'bagel.outgress.permit.v2.probe'
   # terminal B — on outgress pod A's node
   nats --server nats://nats-leaf.<nodeA>:4222 --user outgress_rpc --password $PW \
     pub 'bagel.outgress.permit.v2.probe' hi
   ```
   Arrives ⇒ cross-node interest works, borrow is fine (watch the borrow logs at
   `outgress ... leases`). Silent ⇒ continue.

4. **App-visible signal:** after the Valkey membership fix ships, a healthy fleet
   logs `lease plan committed members=3`. If borrow still never succeeds under a
   single-node burst while members=3, cross-node delivery is the culprit.

## Remediate by outcome

- **Step 1 shows `num_routes: 0` (leaf mesh not formed):** cross-node `6222` is
  blocked. Almost always the known firewalld regression — a node reboot drops the
  runtime-only `cni0`/`flannel.1` interfaces from the `trusted` zone, so
  pod-to-pod cross-node dies. Re-add them **`--permanent`**:
  ```sh
  firewall-cmd --permanent --zone=trusted --add-interface=cni0
  firewall-cmd --permanent --zone=trusted --add-interface=flannel.1
  firewall-cmd --reload
  ```
  Also confirm `nats-leaf-peers` resolves every leaf pod IP
  (`kubectl -n production get endpoints nats-leaf-peers`) and that Linkerd keeps
  `6222` opaque (a proxy that protocol-detects `6222` would stall the route).

- **Step 2 shows the OUTGRESS leaf remote missing/flapping:** `7422` to
  `nats:7422` is blocked or the `leaf_outgress` cred is wrong — check
  `nats-auth-env` and the leafnode authorization user in [nats-server.conf](nats-server.conf).

- **Steps 1–2 healthy but step 3 is silent:** genuine NATS leaf+cluster hybrid
  interest gap. Validate the hub relay in isolation by pointing the probe sub/pub
  at the **hub** (`nats:4222`) instead of the leaves; if that works but leaf↔leaf
  does not, prefer the hub path for this account, or revisit `no_advertise` on the
  leaf cluster so a post-rollout leaf is re-dialed into the mesh.

## Why not just re-route borrow through the hub in code

Because the fix belongs at the layer that is broken. Correctness is already safe
(Valkey membership + `OUTGRESS_LEASE_MIN_MEMBERS=2`); reworking borrow to avoid
cross-node NATS would mask a fleet-wide infra fault that also degrades any future
cross-node RPC. Restore cross-node interest, keep borrow simple.
