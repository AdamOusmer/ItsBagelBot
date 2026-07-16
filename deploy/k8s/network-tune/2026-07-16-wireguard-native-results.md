# Native WireGuard pod data plane — 2026-07-16

## Decision

Keep Tailscale for SSH and explicit private routes: Kubernetes management,
private admin ingress, and the external database. Do not carry pod, Service,
NATS, Valkey, RPC, stream, telemetry, or autoscaling data inside Tailscale.

K3s Flannel now uses the supported `wireguard-native` backend. Every node
advertises a direct WAN endpoint:

| Node | Flannel interface | Endpoint |
|---|---|---|
| node1 | `enp0s6` | `204.216.111.184:51820` |
| node2 | `eth0` | `144.217.7.48:51820` |
| node3 | `eth0` | `148.113.191.17:51820` |
| worker1 | `wlp2s0` | `174.88.117.68:51820` |

The Kubernetes InternalIP remains the node's Tailscale management address. On node2,
`advertise-address: 100.95.95.9` and the API server's kubelet address preference
keep the API Service and logs on InternalIP. The Flux-owned metrics-server also
prefers InternalIP, so kubelet metrics do not require public port 10250.

UDP 51820 is open for kernel WireGuard. Obsolete public VXLAN UDP 8472 is
closed. Native TLS remains enabled at the NATS and Valkey application layers.

Valkey no longer announces node InternalIPs. Its primary, replicas, and four
Sentinels use stable `valkey-node-N.valkey-headless.valkey.svc.cluster.local`
identities over the WireGuard pod network. Host ports and Tailnet certificate
SANs were removed; one-per-node placement is enforced with pod anti-affinity.

## Migration finding

`node-external-ip` also changes the K3s API advertise default. The first server
restart therefore pointed the built-in `kubernetes` Service at the blocked
public node2 address. The permanent fix is explicit:

```yaml
advertise-address: 100.95.95.9
kube-apiserver-arg: ["kubelet-preferred-address-types=InternalIP,Hostname,ExternalIP"]
```

Do not remove those lines while Tailscale is the management/DB plane.

## Pod-network latency

Sequential 400-packet probes after migration:

| Direction | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|
| node2 → node3 | 0.907 ms | 2.418 ms | 4.707 ms | 51.498 ms |
| node3 → node2 | 0.888 ms | 1.576 ms | 3.222 ms | 7.048 ms |
| worker1 → node2 | 4.913 ms | 6.712 ms | 7.753 ms | 13.885 ms |
| node2 → worker1 | 5.875 ms | 7.418 ms | 9.418 ms | 11.266 ms |
| node1 → node2 | 1.970 ms | 2.500 ms | 3.326 ms | 7.831 ms |
| node2 → node1 | 2.023 ms | 2.724 ms | 4.011 ms | 5.989 ms |

The load-bearing node3 → node2 return path improved from 12.117 ms p99 under
VXLAN-over-Tailscale to 3.222 ms p99 under direct kernel WireGuard: a 73%
reduction. worker1 remains bounded by its physical Wi-Fi/WAN path.

## Exact NATS R1/R3 comparison

All runs used the same production hardware and isolated TLS quorum, direct
node2 leader, atomic batch 64, two publisher connections per node, S2-fast
routes, route pool 3, deduplication off, 256-byte varied payloads, and a
12,000 events/s target.

### Before versus after, 20 seconds

| Test | Throughput | node2 p99 | node3 p99 | worker1 p99 | Fleet p99 | Errors |
|---|---:|---:|---:|---:|---:|---:|
| R1 before | 11,992.2/s | 2.802 ms | 6.236 ms | 10.669 ms | 10.669 ms | 0 |
| R1 after | 11,994.0/s | 1.963 ms | 6.061 ms | 13.384 ms | 13.384 ms | 0 |
| R3 before | 11,991.0/s | 11.319 ms | 17.644 ms | 22.741 ms | 22.741 ms | 0 |
| R3 after | 11,992.8/s | 5.578 ms | 7.435 ms | 13.511 ms | 13.511 ms | 0 |

Direct WireGuard reduced the exact R3 fleet p99 by 40.6%, node2 p99 by 50.7%,
and node3 p99 by 57.9%. The R1 fleet number is dominated by normal worker1
Wi-Fi tail variation; node2 improved and node3 was flat.

### R3 60-second soak

| Acked | Throughput | node2 p99 | node3 p99 | worker1 p99 | Fleet p99 |
|---:|---:|---:|---:|---:|---:|
| 720,000 | 11,996.6/s | 7.891 ms | 8.761 ms | 13.681 ms | 13.681 ms |

The soak had zero errors, timeouts, reconnects, disconnects, asynchronous
errors, or leader changes. Peak follower lag was 14 and final lag was zero.

## Valkey native-path result

The same shared Go client was measured before and after moving replication and
Sentinel off Tailscale. Each result used five callers, 1,000 warmups and 10,000
measured operations per node.

| Node/path | Before throughput | After throughput | Before p99 | After p99 |
|---|---:|---:|---:|---:|
| node1 local read | 17,880/s | 33,543/s | 1.201 ms | 0.712 ms |
| node1 primary write | 1,439/s | 1,939/s | 11.431 ms | 7.872 ms |
| node2 local read | 18,225/s | 25,154/s | 0.878 ms | 0.700 ms |
| node2 primary write | 2,490/s | 4,645/s | 5.857 ms | 3.467 ms |

After-only checks were node3 local read 20,764/s at 0.790 ms p99 and primary
write 6,859/s at 2.416 ms p99; worker1 local read 52,611/s at 0.247 ms p99 and
primary write 763/s at 10.207 ms p99. All measured operations completed with
zero errors. The remaining write tail follows physical distance to the current
primary; the local read path is sub-millisecond p99 on every node.

## Production gate

Keep the hot stream at R1. The R3 topology is now stable at 12k/s and the two
datacenter nodes are at or near the old 8 ms objective, but the fleet-wide p99
is still 13.681 ms because worker1 publishes over Wi-Fi. Do not promote R3
until either:

1. the accepted fleet-wide latency objective is raised to about 14 ms; or
2. worker1 leaves the synchronous publish-ack path (wired uplink, datacenter
   replacement, or ingress placement change).

The direct WireGuard data plane should remain: it improves general cross-node
latency and removes the avoidable VXLAN-over-userspace-WireGuard nesting even
while production streams stay R1.
