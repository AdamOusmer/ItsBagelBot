---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Hardware & cluster
description: The control-plane and worker nodes, K3s orchestration, priority classes, and how the cluster is shaped.
---

The cluster is intentionally small and self-hosted. This page documents the physical
layout, the Kubernetes distribution choice, and the scheduling model every workload inherits.

## Physical layout

The cluster has one control-plane node and a pool of worker nodes.

- **`cp1`** is a 2-core ARM node that runs the K3s server. It is tainted, so first-party
  workloads do not schedule on it. Node labels use the key `role`, and `cp1` carries `role=cp`.
- **Workers** carry `role=node` and run every workload. NATS runs one member per worker
  (three), and most services run three replicas spread across them.
  Manifests exclude any node whose `role` is `cp` or `worker` from the production services
  (`deploy/k8s/*.yaml`, `affinity` blocks).

Workloads select nodes by `role`, never by hostname (`deploy/k8s/workloads_test.go`,
`TestNoWorkloadSelectsNodesByHostname`).

Images are built natively for both Intel and ARM on GitHub runners
(`.github/workflows/publish-images.yml`), so each node runs the image for its own architecture.

Pod and Service networking runs on Cilium in native routing mode with kube-proxy replacement
(`deploy/cilium/values.yaml`). Tailscale is reserved for private access: the Kubernetes API
server address in the Cilium values is a tailnet address, and the `tailscale` namespace
carries the private proxies. NATS, Valkey, RPC and telemetry stay on the pod network.
See [Networking →](/infrastructure/networking/).

## Kubernetes distribution: K3s

We run [K3s](https://k3s.io/) rather than upstream Kubernetes. Reasons:

- **Single-binary install.** Bootstrapping a node is one command.
- **Lightweight footprint.** The control plane is small enough to share a 2-core node.
- **SQLite-backed datastore.** The control plane is a single server, not an HA control plane.
  The datastore is the K3s SQLite database (kine), and `deploy/k8s/backup-k3s.yaml` snapshots it
  with `sqlite3 .backup`, verifies it, and ships it off the cluster.

## Scheduling model

Every first-party Deployment declares requests and limits in its manifest, and every
first-party pod carries one of four priority classes (`deploy/k8s/priorityclasses.yaml`):

| Priority class | Value | Used by |
|---|---|---|
| `bagel-data-plane` | 900000 | Valkey and NATS |
| `bagel-service` | 800000 | twitch-ingress, sesame, outgress, projector, gossip, and the database-backed services |
| `bagel-edge` | 700000 | The dashboard and admin consoles |
| `bagel-operator` | 600000 | Cluster tooling such as KEDA and the deployer |

The order is deliberate: services depend on the data plane, so the data plane is evicted last.
A console that is briefly unschedulable costs an operator a page refresh, while evicting a
runtime service drops live chat traffic.

OOM-kills are treated as a load signal, not as failures: the response is to raise the limit of
the service that was killed, not to add slack everywhere.

### Namespaces

| Namespace | Contents |
|---|---|
| `app` | twitch-ingress, sesame, outgress, the Discord services, gossip, the consoles |
| `db` | The database-backed services (users, commands, modules, loyalty, transactions, notifications, discord-data), the projector, and the backup jobs |
| `cache` | Valkey with Sentinel |
| `messaging` | NATS hubs and leaf nodes |
| `ops` | The deployer |
| `operators` | cloudflared and the Doppler operator |
| `networking` | Traefik |
| `tailscale`, `keda`, `observability` | Tailscale proxies, KEDA, and the New Relic agents |

## Spreading and placement

- **Hard spread.** Multi-replica services use a `kubernetes.io/hostname` topology spread with
  `DoNotSchedule`, scoped by `pod-template-hash`, and no `minDomains`. Both choices come from
  incidents recorded in `TestSpreadIsHardWithoutMinDomains`.
- **NATS and Valkey** run as StatefulSets with required anti-affinity, so a node loss costs one
  member and never the quorum (`deploy/messaging/nats.yaml`, `deploy/cache/valkey.yaml`).
- **cloudflared** runs as a DaemonSet on every non-control-plane node
  (`deploy/operators/cloudflared.yaml`), so the loss of a node does not sever public ingress.
- **Pinned volumes** are avoided. A local-path volume strands its pod when the node dies, so
  stateful members rebuild from replication instead.

## What does not run on this cluster

- **CI builds.** GitHub Actions builds the images and pushes them to GHCR. The cluster only
  pulls. See [Getting started](/guides/getting-started/) for the local workflow.
- **The relational database.** MySQL HeatWave is a managed service
  ([ADR 0005](/adr/0005-adoption-of-mysql-heatwave/)). The `backup-mysql` job in the `db`
  namespace dumps it every six hours to object storage.
- **Observability storage.** Logs and metrics ship to New Relic
  ([ADR 0010](/adr/0010-adoption-of-new-relic-for-observability/)), so a cluster outage stays debuggable.

## Capacity headroom

Expansion decisions follow the gates in the
[performance roadmap](/infrastructure/performance-roadmap/): no new server, sharding or
replacement until the verified operating envelope exceeds 70% for 15 minutes or a latency
objective fails after tuning.

## Where to next

- **[Networking →](/infrastructure/networking/)**: Cilium, Tailscale and Cloudflare Tunnel details.
- **[ADR 0004 →](/adr/0004-adoption-of-oracle-cloud/)**: the hosting decision.
- **[ADR 0002 →](/adr/0002-adoption-of-go-as-primary-service-language/)**: why Go services shape the resource model.
