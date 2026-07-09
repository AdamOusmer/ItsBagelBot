# Runbook: transform node1 (OCI ARM) into a dedicated DB node

Forward-looking plan. **Not started.** Captures the sequence so it is ready when
the trigger fires. Written 2026-07-09.

## Current fleet (baseline for this runbook)

| Node | Host | Role today |
|------|------|-----------|
| node1 | OCI ARM, 2c / 12GB | Infra anchor + valkey **backup** replica + nats member. Walled off from hot-path. Destined to become DB-only. |
| node2 | OVH, 6c | k3s server (single sqlite control plane) + hot-path + valkey **master** + nats member. |
| node3 | OVH, 6c | Hot-path (added 2026-07-09). No valkey replica yet. |
| worker1 | home, 8c | Disposable hot-path + valkey **read-only** replica (`replica-priority 0`, never promoted) + nats. |

- DB today: managed **MySQL HeatWave** (external). Services own per-schema tables
  (see `project_mysql_db_grants`), so migration is schema-by-schema.
- End-state: node1 = self-hosted MySQL only; OVH 1/2/3 (node2/node3/OVH3) = etcd
  HA control plane; worker1 = flaky disposable compute.

## Do NOT start until ALL three hold

1. **HeatWave is actually full** (its tier caps you; that is the trigger, not a date).
2. **You are present** to babysit (never during travel).
3. **OVH3 exists** (preferred) so etcd = 3 OVH and node1 is not needed as a server.

## Pre-reqs

- valkey has promotable replicas on **node2 AND node3** (so node1's valkey can be
  removed without losing a compute-node master). Bump the StatefulSet + add node3.
- nats/JetStream R3 has members on node2/node3 (+worker1) so node1's nats can drain
  without losing quorum or stranding stream leaders.
- Spare CPU/mem on node2/node3 to receive node1's singletons.

## Sequence (each step reversible until the DB cutover in step 4)

1. **Valkey off node1.** Master is already on node2 (controlled failover 2026-07-09).
   Add a node3 replica, raise node1's `replica-priority` (de-prefer, e.g. 200) so
   master stays on node2/node3, then remove node1 from the valkey StatefulSet.
   Confirm: master on node2/node3, worker1 still `priority 0`, no split brain.
2. **NATS off node1.** Confirm JetStream stream + meta leaders are on node2/node3,
   then drain node1's `nats` + `nats-leaf`. Recheck quorum and leaders.
3. **Relocate node1 singletons** to node2/node3, one at a time, verifying health of
   each before moving the next: Flux controllers, cloudflared, doppler-operator,
   tailscale operator, ts-ingress, a CoreDNS replica, linkerd control-plane pods.
   traefik/newrelic/repair-controller are DaemonSets and follow automatically.
4. **DB cutover (point of no return).** Snapshot HeatWave first. Stand up
   self-hosted MySQL on node1 (per-service schemas). Migrate schema-by-schema, then
   flip each service's DSN (Doppler) from HeatWave to node1 MySQL and validate that
   service before the next.
5. **Fence node1 as DB-only.** `kubectl drain node1 --ignore-daemonsets
   --delete-emptydir-data`, label/taint `role=db:NoSchedule`, keep only the MySQL
   workload tolerating it.
6. **Decommission HeatWave** once every service is validated on node1 MySQL.

## Safety

- Steps 1-3 are reversible. Step 4 is not without the HeatWave snapshot. Never run
  3-4 while traveling.
- node1 is 2 cores: MySQL only, nothing else co-resident.
- HA control-plane (sqlite -> embedded etcd, 3 OVH servers) is a **separate** project
  from this DB move; do not couple them in the same maintenance window.
