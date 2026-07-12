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
- `network-policies.yaml` `default-deny-apps` selects only app pods, **not**
  `nats`/`nats-leaf`, so NATS pods are NetworkPolicy-unrestricted. NATS and its
  leaves are out of Linkerd; native TLS protects 4222/6222/7422.

RPC is deliberately hub-independent. A break is therefore runtime on the
leaf↔leaf routes (6222), account import/export mapping, or host networking — not
on the hub leafnode listener.

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

2. **Only the BUS bridge is up?** Each leaf should expose exactly one hub
   leafnode remote, for `BUS`. RPC accounts must not appear here.
   ```sh
   kubectl -n production exec <a-leaf-pod> -c nats -- \
     wget -qO- localhost:8222/leafz | grep -E '"leafnodes"|BUS|_RPC'
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
  (`kubectl -n production get endpoints nats-leaf-peers`) and that the peer
  certificates validate on native-TLS port 6222.

- **Step 2 shows any `*_RPC` remote:** the old hub bridge is still loaded.
  Confirm the generated `nats-leaf-config` contains only
  `NATS_LEAF_REMOTE_URL_BUS`, then reload/restart that leaf.

- **Step 1 healthy but step 3 is silent:** inspect the account's service
  imports/exports in `nats-auth.conf`, then revisit `no_advertise` if a
  post-rollout leaf was not re-dialed into the mesh. Do not mask the failure by
  restoring an RPC hub remote.

## Why not just re-route borrow through the hub in code

Because the fix belongs at the layer that is broken. Correctness is already safe
(Valkey membership + `OUTGRESS_LEASE_MIN_MEMBERS=2`); reworking borrow to avoid
cross-node NATS would mask a fleet-wide infra fault that also degrades any future
cross-node RPC. Restore cross-node interest, keep borrow simple.
