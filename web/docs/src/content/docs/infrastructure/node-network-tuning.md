---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Worker network tuning
description: "Eight-queue NIC configuration, IRQ and XPS CPU maps, offloads, persistence, and measured limits on node1–3."
docType: reference
---

The worker network configuration uses eight combined RX/TX queue pairs on both `eth0` and `eth1`. NetworkManager stores the NIC settings; one systemd oneshot applies IRQ affinity, XPS and RPS settings at boot. There is no dispatcher hook, watchdog or recurring repair check.

This page records the configuration applied on **2026-11-09** to `node1`, `node2` and `node3`: AlmaLinux 10.2 guests with 12 online vCPUs and virtio NICs. `cp1` was outside the change. The settings and measurements describe that fleet and date, rather than a general-purpose tuning preset.

## What changed and why

| Area | Previous state | Final state and reason |
| --- | --- | --- |
| eth0 queue persistence | Eight queues were already active, but the boot script still assumed one queue | Save eight queues in the NetworkManager profile and replace the stale IRQ policy |
| eth0 interrupts | Several queues received on one CPU and completed transmits on another | Pin each RX/TX pair to the same vCPU, following eth1's mapping |
| Receive steering | The old eth0 boot policy would enable RPS across all CPUs | Set every RX queue's RPS mask and RFS count to zero; avoid an extra software handoff after queue delivery |
| Transmit selection | eth0 used the driver's original CPU masks | Apply eth1's XPS masks to both NICs, matching the interrupt CPU for vCPUs 2–9 |
| eth0 TX queue length | `1000` | `5000`, matching eth1's microburst headroom |
| eth0 UDP GRO forwarding | Off | On, with GRO list off, matching the forwarding offload policy |
| eth0 IPv4 GRO maximum | `65536` bytes after device recreation | `196608` bytes, reconciled through the existing Cilium BIG TCP policy |
| Persistence | Overlapping startup scripts, including the stale single-queue assumption | Saved NIC profiles plus one boot-time IRQ service; retire the adaptive and eth1-specific services |
| FQ flow-limit trial | Cilium's active leaves used `100` packets per flow | Keep `100`; the `1000`-packet trial did not establish a benefit |

The goal was to use all eight queues while keeping queue work local to its assigned CPU. Aligning IRQs and XPS can reduce avoidable cross-CPU work; it does not pin applications or prove that all cache invalidations have disappeared. The [Linux networking scaling guide](https://www.kernel.org/doc/html/latest/networking/scaling.html) explains the separate roles of RSS, RPS, RFS and XPS.

## Configuration ownership and persistence

| Owner | Configuration | Application point |
| --- | --- | --- |
| NetworkManager | Queue count, ring sizes, checksum/segmentation offloads, UDP GRO and TX queue length | Existing connection profiles on activation |
| `fleet-irq-affinity.service` | IRQ CPU targets, XPS CPU masks, RPS/RFS disabled | Once per boot, after NetworkManager is online and before k3s starts |
| Existing host sysctl policy | Socket buffers, backlog, BBR, default FQ and other network defaults | Host startup; see the dated snapshot below |
| Cilium | BIG TCP limits, FQ pacing, native routing and BPF data path | Agent initialization and normal Cilium reconciliation |

[`configure-network-profiles.sh`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/configure-network-profiles.sh) selects each device's existing active connection UUID and writes its settings with `nmcli connection modify`. It does not bounce a link or reactivate a connection. The live values were applied during the rollout, then the saved profiles were reloaded from disk and checked again.

[`fleet-irq-affinity.service`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/fleet-irq-affinity.service) is `Type=oneshot`, with `RemainAfterExit=yes`. It runs `/usr/local/sbin/fleet-irq-affinity.sh` after `network-online.target` and `NetworkManager-wait-online.service`, before `k3s.service` and `k3s-agent.service`, and is enabled under `multi-user.target`.

`irqbalance.service` remains masked. Both `bagelbot-network-performance.service` and `fleet-eth1-tuning.service` are disabled. The proposed NetworkManager dispatcher hook was removed. Manual verification is separate from configuration and has no scheduled unit.

## NIC settings

Both devices advertise a maximum of eight combined queue pairs, 1024 RX ring descriptors and 256 TX ring descriptors. Those maxima were already active; the profile change makes them explicit and persistent.

| Setting | NetworkManager property | eth0 | eth1 |
| --- | --- | --- | --- |
| Combined queues | `ethtool.channels-combined` | `8` | `8` |
| RX ring descriptors | `ethtool.ring-rx` | `1024` | `1024` |
| TX ring descriptors | `ethtool.ring-tx` | `256` | `256` |
| RX checksum | `ethtool.feature-rx` | `on` | `on` |
| TX checksum | `ethtool.feature-tx` | `on` | `on` |
| Scatter/gather | `ethtool.feature-sg` | `on` | `on` |
| TCP segmentation offload | `ethtool.feature-tso` | `on` | `on` |
| Generic segmentation offload | `ethtool.feature-gso` | `on` | `on` |
| Generic receive offload | `ethtool.feature-gro` | `on` | `on` |
| UDP GRO forwarding | `ethtool.feature-rx-udp-gro-forwarding` | `on` | `on` |
| GRO list | `ethtool.feature-rx-gro-list` | `off` | `off` |
| TX queue length | `link.tx-queue-length` | `5000` | `5000` |
| Link MTU, preserved | Existing connection configuration | `1500` | `9000` |

`eth0` carries the public path; `eth1` carries private worker traffic and Cilium's direct routing. The public link was not changed to jumbo MTU. Hardware GRO was also observed enabled on both devices, but the profile installer does not explicitly set that feature.

These are **virtio guest queues**. The hypervisor does not expose configurable RSS indirection: `ethtool -x` reports operation not supported for the table and key. Eight active queues do not establish control over the physical NIC's RSS table or host-side vhost threads.

## IRQ, XPS and receive steering

The same map applies to both NICs on all three workers. CPU numbers are guest vCPUs.

| Queue | RX IRQ target | TX completion IRQ target | `xps_cpus` hex mask | CPUs using this XPS queue |
| --- | --- | --- | --- | --- |
| 0 | 2 | 2 | `005` | 0, 2 |
| 1 | 3 | 3 | `008` | 3 |
| 2 | 4 | 4 | `010` | 4 |
| 3 | 5 | 5 | `020` | 5 |
| 4 | 6 | 6 | `040` | 6 |
| 5 | 7 | 7 | `080` | 7 |
| 6 | 8 | 8 | `500` | 8, 10 |
| 7 | 9 | 9 | `a00` | 9, 11 |

Every RX queue has `rps_cpus=0` and `rps_flow_cnt=0`. Receive-queue-based XPS, `xps_rxqs`, was observed as zero; this configuration uses CPU-based XPS. CPU 1 has no explicit XPS mask entry and uses the kernel's fallback transmit selection. CPUs 0, 10 and 11 share the listed queues with their completion CPU. There are eight queue pairs for twelve vCPUs, so the configuration cannot give every vCPU an exclusive pair.

[`fleet-irq-affinity.sh`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/fleet-irq-affinity.sh) resolves virtio devices through sysfs and matches IRQ names such as `virtioN-input.3` and `virtioN-output.3`. IRQ numbers are not hardcoded. It requires at least twelve online CPUs and exactly eight RX and TX queues per NIC. Balloon IRQs target CPU 0; storage IRQs target CPU 11. A kernel-managed storage IRQ is accepted only if its effective CPU is already 11.

This is interrupt affinity, not CPU isolation. Application threads may run on any scheduler-eligible vCPU. No application `cpuset`, Kubernetes CPU Manager allocation, physical-core pinning or vhost-thread affinity was added. The guest reports one shared L3 domain; physical host cache and NUMA placement remain unverified.

## Cilium routing, pacing and BIG TCP

The following existing policy was preserved in [`deploy/cilium/values.yaml`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/cilium/values.yaml) and [`node-configs.yaml`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/cilium/node-configs.yaml):

| Setting | Value |
| --- | --- |
| `bandwidthManager.enabled` / `bandwidthManager.bbr` | `true` / `true` |
| `bpf.hostRouting` / `bpf.masquerade` | `true` / `true` |
| `enableIPv4BIGTCP` / `pmtuDiscovery.enabled` | `true` / `true` |
| `routingMode` / `autoDirectNodeRoutes` | `native` / `true` |
| `ipv4NativeRoutingCIDR` and pod address pool | `10.42.0.0/16` |
| `kubeProxyReplacement` / `loadBalancer.serviceTopology` | `true` / `true` |
| Worker devices / direct routing device | `eth0 eth1 tailscale0` / `eth1` |
| Worker Cilium MTU | `9000`; public eth0 link MTU remains `1500` |
| BPF trace / policy-verdict events / drop events | Disabled / disabled / enabled |
| Hubble / L7 proxy / Envoy | Disabled / disabled / disabled |
| Cilium rolling update | `maxUnavailable: 1` |

Each NIC has an `mq` root with eight `fq` leaves. Observed leaf parameters were `limit=10000`, `flow_limit=100`, `buckets=32768`, `horizon=2s` and `horizon_drop`. Host TCP congestion control remains `bbr`; busy polling remains off.

After rolling the worker Cilium agents one at a time, both NICs had `gro_ipv4_max_size=196608` and `gso_ipv4_max_size=65536`. The NIC advertises `tso_max_size=65536`. The larger GRO limit batches receive work inside the stack; it does not enlarge packets on the public wire. See [Cilium's BIG TCP tuning guidance](https://docs.cilium.io/en/stable/operations/performance/tuning/).

## Existing host network policy

The queue rollout did **not** replace the wider host sysctl policy. The following is the contents observed in `/etc/sysctl.d/99-fleet-network-tuning.conf` during the audit. It records configured values, not a claim that every entry was independently checked against every later override. The older repository example [`90-bagelbot-net.conf`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/90-bagelbot-net.conf) contains smaller buffers and backlog; it was not used to overwrite this fleet policy.

```ini
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
net.core.rmem_default = 212992
net.core.wmem_default = 212992
net.core.optmem_max = 2097152
net.ipv4.tcp_rmem = 4096 131072 67108864
net.ipv4.tcp_wmem = 4096 16384 67108864
net.ipv4.udp_rmem_min = 16384
net.ipv4.udp_wmem_min = 16384
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65536
net.ipv4.tcp_abort_on_overflow = 0
net.core.netdev_budget = 1200
net.core.netdev_budget_usecs = 8000
net.core.dev_weight = 64
net.core.gro_normal_batch = 16
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_notsent_lowat = 131072
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_no_metrics_save = 1
net.ipv4.tcp_window_scaling = 1
net.ipv4.tcp_sack = 1
net.ipv4.tcp_dsack = 1
net.ipv4.tcp_fastopen = 3
net.ipv4.ip_local_port_range = 10240 65535
net.ipv4.ip_local_reserved_ports = 30000-32767
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_max_tw_buckets = 65536
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_synack_retries = 3
net.core.busy_read = 0
net.core.busy_poll = 0
net.ipv4.tcp_autocorking = 1
net.core.rps_sock_flow_entries = 32768
net.ipv4.neigh.default.gc_thresh1 = 4096
net.ipv4.neigh.default.gc_thresh2 = 8192
net.ipv4.neigh.default.gc_thresh3 = 16384
net.ipv4.conf.eth1.arp_ignore = 1
net.ipv4.conf.eth1.arp_announce = 2
net.ipv4.conf.eth1.rp_filter = 2
net.ipv4.conf.eth1.accept_redirects = 0
net.ipv4.conf.eth1.send_redirects = 0
net.netfilter.nf_conntrack_max = 393216
net.netfilter.nf_conntrack_tcp_timeout_fin_wait = 30
net.netfilter.nf_conntrack_tcp_timeout_time_wait = 30
```

Buffer sizes are bytes: the socket maxima are 64 MiB, with a 65536-packet network backlog and a 1200-packet/8000-microsecond softirq budget. The global RFS table size remains `32768`, while every NIC queue's RFS count is zero. Its presence does not mean per-queue RFS is enabled. eth0 reverse-path filtering was restored to its original strict value, `1`, after the tests.

## Validation and measured limits

The [dated audit](https://github.com/AdamOusmer/ItsBagelBot/blob/main/docs/operations/2026-11-09-node-network-queues.md) and [machine-readable results](https://github.com/AdamOusmer/ItsBagelBot/blob/main/docs/operations/2026-11-09-node-network-queues.json) preserve the environment, method, exclusions and results. There were 101 completed measurement commands. Tests covered every ordered worker pair on each NIC, one and eight TCP streams, CUBIC, bidirectional BBR, UDP stress and pinned UDP processes.

| Observation | Result |
| --- | --- |
| Final eth0 TCP, eight streams | `6.49–8.67 Gbps` across directions |
| eth1 TCP after IRQ alignment, eight streams | `8.70–8.98 Gbps` |
| Verified eth1 bidirectional BBR | Forward `8.34–8.69 Gbps`, reverse `7.03–8.58 Gbps` simultaneously |
| Queue interrupts after alignment | `50,889,863`, with none outside the assigned paired CPU |
| NIC, qdisc and softnet drops in TCP samples | Zero |
| Pinned eth0 UDP at 1 Gbps offered | `1.00 Gbps` delivered, about `0.044%` loss |
| Pinned eth1 UDP at 4 Gbps offered | `4.00 Gbps` delivered, zero reported loss |

IRQ alignment did not establish a consistent TCP speedup. Host-wide cache-miss samples also did not establish an improvement; they are not proof of eliminated L1/L2/L3 invalidations. The verified result is queue placement and durable configuration.

UDP stress at about 10 Gbps was lossy. With one pinned process per queue, node1 → node2 delivered about 3.19 Gbps on eth0 and 5.84 Gbps on eth1. The private sender had zero transmit qdisc drops, while the peer's guest NIC received substantially fewer bytes. This locates a loss component before the receiving guest NIC, without identifying the hypervisor, virtual switch, physical link or provider policer responsible.

The `flow_limit=1000` trial delivered 2.98/5.73 Gbps on eth0/eth1 under pinned UDP, compared with 3.19/5.84 at `100`. The larger limit was discarded. The CUBIC trial also did not justify changing BBR. The measurements do not establish loss-free 10 Gbps capacity.

## Manual verification and operations

Run these read-only commands as root **on a worker**, with `ethtool`, `iproute`, NetworkManager and Python 3 installed. They inspect the current state and saved profiles; they are not a monitoring loop.

```sh
ethtool -l eth0
ethtool -l eth1
ethtool -g eth0
ethtool -g eth1
ethtool -k eth0
ethtool -k eth1
ip -d link show eth0
ip -d link show eth1
tc -s qdisc show dev eth0
tc -s qdisc show dev eth1
systemctl is-enabled fleet-irq-affinity.service
systemctl is-active fleet-irq-affinity.service
/usr/local/sbin/verify-net-queues
```

[`verify-queues.py`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/deploy/k8s/network-tune/verify-queues.py), installed as `/usr/local/sbin/verify-net-queues`, checks active and saved queue settings, requested and effective IRQ masks, XPS, RPS/RFS, FQ leaves, offloads and service state. Its `--allow-pending-irq` option is only for installation: effective IRQ migration can wait for the next interrupt. After traffic, validation must pass without that option.

The rollout needed no node reboot. Cilium agents were restarted one worker at a time; all workers and agents were Ready afterward. No reboot was performed to validate persistence. Original files and runtime masks remain in `/var/tmp/bagelbot-netqueues-20261109/` on each worker. The old script contains the single-queue assumption; `before-runtime.json` records the actual pre-change multiqueue masks.

Benchmark ports, source-restricted firewall rules, temporary reverse-path-filter changes, cleanup timers and privileged diagnostic pods were removed. Large historical UDP stress drop counters remain counters from those tests, rather than evidence of continuing production loss.

Continue with [runtime configuration](/infrastructure/runtime-configuration/) and [performance measurement](/infrastructure/performance/). The [configuration source directory](https://github.com/AdamOusmer/ItsBagelBot/tree/main/deploy/k8s/network-tune) contains the installer, IRQ policy, unit and manual validator.
