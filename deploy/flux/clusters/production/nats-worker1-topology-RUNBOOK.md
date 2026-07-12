# NATS topology migration + prod e2e test

Move the JetStream hub off the 2-core node1 and onto worker1, free node1 of
Flux, then validate the new topology end to end on prod **before** raising any
resource limits. "200k" is a direction, not an SLA: the gate is a healthy,
non-degrading bus under real load, not a specific number.

## What changes (already staged in the branch)

- `deploy/k8s/nats.yaml`: hub affinity `NotIn worker1` -> `NotIn node1`, plus a
  toleration for `itsbagelbot.dev/pool=worker-pool`. Members move to
  node2 / node3 / worker1. The post-migration R1 sizing keeps modest requests
  (cpu 500m, memory 2Gi) with burst/retention limits of 2 cores and 6Gi;
  JetStream itself is capped at 4GB.
- `deploy/flux/clusters/production/flux-system/kustomization.yaml`: nodeAffinity
  `NotIn node1` on every Flux controller.
- `pkg/bus/provision.go`: TWITCH_INGRESS + TWITCH_OUTGRESS memory-backed
  (create-only: existing streams must be recreated to convert).
- `app/ingress/.../nats/publisher*.ex`: sharded async publisher.

## Constraints that shape the steps

- **Storage is node-pinned.** `jetstream-data-*` are `local-path` PVCs bound to
  a node's disk. Moving the node1 member means deleting `jetstream-data-nats-2`
  so a fresh PVC provisions on worker1. That member's local stream data is lost
  and re-syncs: R3 streams (TWITCH_INGRESS, TWITCH_OUTGRESS_SYSTEM) rebuild from
  their peers; **R1 streams (BAGEL_DATA, TWITCH_OUTGRESS) lose their buffer if
  their single copy sat on nats-2** — perishable (5m / 5s), but confirm before
  deleting.
- **worker1 rides home wifi.** With 3 voters a blip keeps node2+node3 as a 2/3
  quorum, so R3 survives. An **R1** stream whose one replica lands on worker1
  stalls while the link blips — the key thing the soak test must expose.
- **Flux watches `main`.** These manifest changes reach prod only when merged to
  main. The PVC/stream/pod steps below are manual `kubectl`/`nats` ops that Flux
  does not perform.

## Phase A — Flux off node1 (low risk, independent, do first)

1. Merge the flux-system kustomization change to main.
2. Flux reschedules its own controllers. Verify none remain on node1 and
   reconciliation still works:
   ```
   kubectl -n flux-system get pods -o wide          # none on node1
   flux get kustomizations                           # all Ready, reconciling
   ```

## Phase B — NATS onto worker1

3. Pre-flight snapshot (so you can tell re-sync from loss afterwards):
   ```
   nats stream ls
   for s in TWITCH_INGRESS TWITCH_OUTGRESS TWITCH_OUTGRESS_SYSTEM BAGEL_DATA; do
     nats stream info "$s" --json | jq '{name:.config.name,replicas:.config.num_replicas,msgs:.state.messages,leader:.cluster.leader,peers:[.cluster.replicas[].name]}'
   done
   kubectl get nodes -o wide                          # node2/node3/worker1 headroom
   ```
   If BAGEL_DATA / TWITCH_OUTGRESS are R1 with their copy on nats-2 and the
   buffer matters, temporarily raise them to R3 first (`nats stream edit --replicas 3`).
4. Merge the nats.yaml affinity/toleration change to main. (A StatefulSet does
   not reschedule running pods on an affinity change — nats-2 stays on node1
   until step 5.)
5. Move the node1 member. Its PVC is pinned to node1, so the PVC must go too:
   ```
   kubectl -n production delete pvc jetstream-data-nats-2
   kubectl -n production delete pod nats-2
   ```
   The one-per-node spread (node2=nats-1, node3=nats-0) pushes the fresh nats-2
   onto worker1.
6. Verify:
   ```
   kubectl -n production get pod nats-2 -o wide       # Running on worker1
   nats server report jetstream                        # 3 peers, all current
   nats stream info TWITCH_INGRESS --json | jq '.cluster'   # replica caught up
   ```

## Phase C — convert transient streams to memory (create-only)

7. Deploy the new `pkg/bus` image first (so `EnsureStreams` specs say memory),
   then recreate the two streams during the window so they come back
   memory-backed (perishable, safe to drop briefly):
   ```
   nats stream rm TWITCH_INGRESS
   nats stream rm TWITCH_OUTGRESS
   # EnsureStreams (running in the services) recreates them; confirm storage:
   nats stream info TWITCH_INGRESS --json | jq '.config.storage'   # "memory"
   ```
   Keep TWITCH_INGRESS **R1** for throughput unless the soak shows worker1
   stalls the firehose, in which case make it R3.

## Phase D — async ingress publisher (BUS now direct to hub)

The ingress firehose plane (`:gnat_bus` + the PublisherPool connections) now
dials the hub `nats` service directly, not `nats-leaf` (NATS_HUB_HOST=nats). RPC
stays on the leaf. So this deploy both switches to the async publisher and moves
the firehose off the leafnode hop.

8. Deploy the new ingress image + twitch-ingress.yaml env. Confirm health and
   that publishes flow to the hub:
   ```
   kubectl -n production rollout status statefulset/twitch-ingress
   # Confirm BUS connections land on hub pods, not the leaf:
   kubectl -n production exec nats-0 -c nats -- \
     wget -qO- localhost:8222/connz | grep -c ingresspub   # inbox subs present
   # New Relic: Nats/PublishAcked climbing, Nats/PublishInflight bounded,
   # Nats/PublishOverloaded == 0, Nats/PublishFailed == 0
   ```

## Phase E — END-TO-END TEST (the gate before any bump)

9. **Functional:** in a test channel, exercise a real round-trip — a `!`command
   and a plain chat line that should trip automod — and confirm the bot responds
   (ingress -> sesame -> outgress -> Twitch).
10. **Soak (>= 15-30 min, to catch worker1 wifi blips):** drive elevated volume
    (or ride an organic peak) and watch:
    - publish errors == 0; `Nats/PublishOverloaded` == 0; dispatcher drops == 0
    - PubAck p50/p95/p99 (latency probe + `Nats/PublishInflight`)
    - consumer lag non-growing and small — KEDA `nats-jetstream` trigger, or
      `nats consumer info TWITCH_INGRESS worker_twitch_ingress_event_standard`
    - RAFT stability: `nats server report jetstream` shows no repeated leader
      changes; no quorum-loss log lines when worker1's link wobbles
11. **Gate:** all green -> Phase F. If worker1 flakiness causes R1 firehose
    stalls or RAFT churn -> pin the hot stream off worker1 / keep it R3, or roll
    back (below).

## Phase F — bump resources (only after Phase E passes)

12. The first resource bump sets requests to cpu 500m / memory 2Gi, limits to
    2 cores / 6Gi, and `max_mem` to 4GB. Raise TWITCH_INGRESS
    `MaxBytes`/`MaxMsgsPer` separately if the retention target needs it.
    MaxBytes is updatable in place (reconcile handles it); `max_mem` and pod
    limits need a NATS roll. Re-run the Phase E soak after each step.

## Rollback

- `git revert` the two manifest changes -> Flux restores `NotIn worker1` and
  Flux-on-node1. nats-2 reschedules to node1 (delete `jetstream-data-nats-2`
  again to unpin).
- If memory storage misbehaves, recreate the streams as file (revert
  provision.go's `Storage` and recreate).
- Perishable streams re-provision themselves via `EnsureStreams`; no replay to
  restore.
