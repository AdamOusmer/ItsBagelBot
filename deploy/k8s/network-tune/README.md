<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Worker NIC queue configuration

This configuration targets node1–3: 12 online vCPUs and eight combined queues
on each virtio NIC. NetworkManager owns the persistent NIC settings. One
systemd oneshot sets IRQ affinity at boot, after NetworkManager has activated
the connections and before k3s starts. There is no dispatcher hook, timer,
watchdog, or recurring repair check.

`configure-network-profiles.sh` saves the following in the existing connection
profiles, selected by the active device's connection UUID:

| Setting | eth0 | eth1 |
| --- | --- | --- |
| Combined queues | 8 | 8 |
| RX / TX ring descriptors | 1024 / 256 | 1024 / 256 |
| TX queue length | 5000 | 5000 |
| Checksum, scatter/gather, TSO, GSO, GRO | on | on |
| UDP GRO forwarding | on | on |
| GRO list | off | off |
| MTU, preserved by this installer | 1500 | 9000 |

The installer updates the saved profiles without bouncing or reactivating a
connection. These values were also applied live during the 2026-11-09 rollout.
NetworkManager applies the saved values on subsequent connection activation.

`fleet-irq-affinity.sh` sets the same CPU plan on both NICs:

| Queue | RX and TX completion CPU | XPS CPU mask |
| --- | --- | --- |
| 0 | 2 | 005 |
| 1 | 3 | 008 |
| 2 | 4 | 010 |
| 3 | 5 | 020 |
| 4 | 6 | 040 |
| 5 | 7 | 080 |
| 6 | 8 | 500 |
| 7 | 9 | a00 |

RPS and per-queue RFS are zero on all RX queues. The masks keep CPUs 2–9 on
their matching completion queue; CPUs 0, 10 and 11 share the listed queues.
CPU 1 has no explicit XPS entry and uses the kernel's fallback selection. Balloon
IRQs remain on CPU 0 and storage IRQs on CPU 11. The script resolves IRQs by
virtio device and queue name rather than hardcoding IRQ numbers. It rejects
an incompatible CPU or queue layout before applying the plan. irqbalance
remains masked, as it was before this change.

The existing host sysctl policy selects `fq`; the multiqueue root has eight
FQ leaves. The existing Cilium configuration owns IPv4 BIG TCP. Its worker
agents were rolled individually to reconcile the recreated eth0 devices:
both NICs now have a 196608-byte IPv4 GRO limit and 65536-byte GSO limit.
The NIC advertises a 65536-byte TSO maximum. Public MTU remains 1500.

Install the profile script and IRQ script under `/usr/local/sbin/`, and the
IRQ unit under `/etc/systemd/system/`. Run the profile installer once, reload
systemd, disable the superseded `bagelbot-network-performance.service` and
`fleet-eth1-tuning.service`, and enable `fleet-irq-affinity.service`. Both
superseded services are disabled on all three workers. The IRQ service owns
the per-queue RPS settings; Cilium owns FQ pacing with its 100-packet per-flow
limit. A 1000-packet trial did not establish a reliable benefit with the
current multiqueue setup, so that override was removed.

`verify-queues.py` is a manual, read-only validation command. It checks both
live state and saved NetworkManager properties. It has no scheduled unit.
Its `--allow-pending-irq` option is for installation only: Linux can defer
effective IRQ migration until the next interrupt. Validation after traffic
must pass without that option.

The test results and remaining limits are recorded in
[the 2026-11-09 audit](../../../docs/operations/2026-11-09-node-network-queues.md).

Backups of the original scripts, units, live masks and NetworkManager profiles
are retained on each worker under `/var/tmp/bagelbot-netqueues-20261109/`.
The matching local test directory contains raw iperf output and interface,
qdisc, interrupt and CPU counter snapshots. Benchmark firewall rules and
listeners are temporary and are removed at the end of the test.
