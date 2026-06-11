---
title: Hardware & cluster
description: ARM nodes, K3s orchestration, resource limits, and how the cluster is shaped.
---

The cluster is intentionally small, intentionally ARM, and intentionally self-hosted. This page documents the physical
layout, the Kubernetes distribution choice, and the resource model every workload inherits.

## Physical layout

All nodes are mixed-architecture: **`node1`** is **`linux/arm64`** (aarch64) and **`node2`** is **`linux/amd64`** (Intel). All workloads are scheduled across both nodes for high availability (HA), using multi-architecture Docker images.

Networking between nodes is over **Tailscale only**. There is no second LAN-only path.
See [Networking →](/infrastructure/networking/).

## Kubernetes distribution: K3s

We run [K3s](https://k3s.io/) rather than upstream Kubernetes. Reasons:

- **Single-binary install.** Bootstrap on a fresh ARM node is one shell command; rebuilding a node from scratch is a
  documented, ~15-minute operation.
- **Lightweight resource footprint.** The K3s server process holds the control-plane in ~512 MB resident; upstream
  Kubernetes would consume a meaningful fraction of a node before any workload runs.
- **SQLite-backed datastore** (we don't run HA control-plane; a single server is acceptable for a single-streamer
  workload). If we ever grow to multi-control-plane, the migration to embedded etcd is a documented K3s path.
- **Sensible defaults stripped out.** We disable Traefik (we use Cloudflare Tunnel instead, so cluster ingress is
  irrelevant) and ServiceLB (Tailscale is our LB-by-identity).

K3s flags of note (`/etc/rancher/k3s/config.yaml`):

```yaml
disable:
  - traefik
  - servicelb
flannel-backend: none      # Tailscale provides node networking
disable-network-policy: false
write-kubeconfig-mode: "0640"
node-label:
  - "node.bagelbot.io/role=worker"
```

A node-local kubeconfig is restricted to the operator group (group `bagelbot`); the kubeconfig distributed to operator
devices binds to the Tailscale interface address of the server, not the LAN address.

## Resource model

Every workload **must** declare requests and limits. This is enforced by a `LimitRange` per namespace and an admission
policy that rejects pod specs missing CPU or memory limits.

### Defaults

```yaml
# applied to any container that doesn't specify
resources:
  requests:
    cpu: "100m"
    memory: "64Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"
```

These are **starting points**, not targets — services are expected to set their own values based on measured load.

### Why we limit aggressively

Native binaries make tight limits realistic ([ADR-0002](/adr/0002-native-compilation/)). A Go service handling a normal
Twitch event stream sits comfortably under 50 MB resident; the GraalVM-compiled Kotlin service sits under 150 MB.
Generous defaults would waste a meaningful fraction of an 8 GB node.

OOM-kills are treated as a load signal, not as failures: if a service is OOM-killed during a hype train, the response is
to raise its limit, not to add slack everywhere.

### Class of workload → namespace

| Namespace         | Class                                                            | QoS expectation                                  |
|-------------------|------------------------------------------------------------------|--------------------------------------------------|
| `bagelbot-system` | Cluster services (cloudflared, tailscale-operator, cert-manager) | Guaranteed; never evicted.                       |
| `bagelbot-data`   | Stateful (Postgres, Redis)                                       | Guaranteed; pinned to nodes with local SSD.      |
| `bagelbot-app`    | The bot services themselves                                      | Burstable; can be evicted under memory pressure. |
| `bagelbot-build`  | Image builds and one-shot jobs                                   | Best-effort; preempted readily.                  |

Pod priority classes line up with the QoS expectation — system pods are `system-cluster-critical`, data pods are a
custom `bagelbot-data-critical`, app pods are default.

## Node selection & tolerations

- **Storage workloads** (Postgres, Redis persistence) carry a `nodeSelector` requiring `node.bagelbot.io/storage=true`.
  This is set on nodes with attached SSD.
- **The cloudflared deployment** carries an anti-affinity rule to spread replicas across nodes; a single-node outage
  shouldn't sever public ingress.
- **GPU / acceleration:** none in the current fleet. If we add a node with a Coral or similar later, it'll get its own
  taint and an explicit toleration on the workloads that use it.

## What does *not* run on this cluster

- **CI builds.** GitHub Actions runs build jobs on hosted ARM runners; the cluster only pulls.
  See [CI/CD →](/infrastructure/cicd-pipeline/).
- **Long-term backups.** Snapshots are pushed off-cluster to [TBD: backup target — e.g., Backblaze B2 via restic]. The
  cluster is treated as ephemeral compute; only data volumes are sacred.
- **Logging/observability storage.** Logs and metrics are shipped
  to [TBD: external Grafana Cloud / self-hosted off-cluster]. Keeping observability *off* the cluster means we can still
  debug a cluster outage.

## Capacity headroom

The cluster targets **~50% steady-state utilization** so that a hype-train burst or a single-node failure doesn't
immediately cause evictions. If sustained utilization climbs above ~65%, the response is to add a node, not to tune
limits down.

## Where to next

- **[Networking →](/infrastructure/networking/)** — Tailscale mesh and Cloudflare Tunnel details.
- **[CI/CD pipeline →](/infrastructure/cicd-pipeline/)** — how images get from a `git push` onto these nodes.
- **[ADR-0001 →](/adr/0001-zero-trust-network/)** — the Zero-Trust networking decision.
- **[ADR-0002 →](/adr/0002-native-compilation/)** — why native binaries shape the resource model.
