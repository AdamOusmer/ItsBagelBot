<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# node1–3 queue and throughput audit — 2026-11-09

All three workers have eight active combined queues on both eth0 and eth1,
matching the maximum advertised by their virtio NICs. The requested eth0 work
is complete. NIC settings are saved in NetworkManager profiles, IRQ affinity
is applied once at boot, and no dispatcher hook, watchdog, recurring check or
repair timer is installed.

No node reboot was required. The three worker Cilium agents were restarted
one at a time to reconcile eth0's recreated devices with the existing BIG TCP
configuration. cp1 was not restarted or modified.

## Changes applied

The old boot-time IRQ script still described eth0 as single-queue. It would
have pinned only queue 0 and enabled RPS across all 12 CPUs. The replacement
pins both NICs' RX and TX completion queue pairs 0–7 to CPUs 2–9, with matching
CPU-based transmit queue selection. RPS and per-queue RFS are zero everywhere.

Both saved Ethernet profiles specify eight combined queues, maximum RX/TX
rings of 1024/256, TX queue length 5000, checksum and segmentation offloads,
GRO, UDP GRO forwarding enabled and GRO list disabled. eth0 previously had TX
queue length 1000 and UDP GRO forwarding disabled. Public MTU remains 1500;
private MTU remains 9000.

Both NICs now have the existing Cilium policy's 196608-byte IPv4 GRO limit.
eth0 had fallen back to 65536 after recreation. IPv4 GSO and the NIC's
advertised TSO maximum remain 65536. Cilium owns BIG TCP and FQ pacing; its
saved configuration was already enabled.

The old adaptive network service and the old eth1 tuning service are disabled.
NetworkManager owns saved NIC attributes and the single IRQ oneshot owns IRQ,
XPS and RPS settings. irqbalance remains masked. Saved profile values were
reloaded from disk and verified again, without reactivating or bouncing links.
A node reboot was not used as a persistence test.

See [the configuration and CPU map](../../deploy/k8s/network-tune/README.md).

## Measurement method

iperf3 3.17.1 ran between every ordered worker pair, on both NICs, with one and
eight TCP streams before and after IRQ alignment. Additional runs covered
reconciled BIG TCP state, per-socket CUBIC, bidirectional BBR, UDP stress,
per-queue pinned UDP processes and lower offered UDP rates.

Both endpoints were bound to the selected device and its address, and NIC
byte counters confirmed the path. This matters because ordinary routes to
peer public addresses prefer eth1. The tests used temporary, source-restricted
firewall rules and temporarily changed eth0 reverse-path filtering from strict
to loose to permit the deliberately asymmetric test setup.

The raw log contains 101 completed measurement commands. Six
listener setup failures were retried successfully. Six earlier duplex runs
mixed CUBIC and BBR because the reused server retained a prior setting; those
runs are explicitly excluded from duplex conclusions. The final six duplex
runs used fresh listeners and explicitly selected BBR at both endpoints.

TCP samples ran for eight measured seconds plus two omitted warmup seconds.
Duplex and UDP samples generally ran for six measured seconds plus warmup.
Results describe this live fleet and time window; one before/after sample per
direction does not establish a small causal speed change.

## TCP results

All figures are delivered application Gbps, across the six ordered pairs.

| State | Interface | Streams | Minimum | Mean | Maximum |
| --- | --- | --- | --- | --- | --- |
| Baseline | eth0 | 1 | 4.21 | 4.90 | 5.49 |
| Baseline | eth0 | 8 | 6.64 | 7.81 | 8.60 |
| IRQ alignment | eth0 | 8 | 6.53 | 7.73 | 8.48 |
| Final BIG TCP / BBR | eth0 | 8 | 6.49 | 7.75 | 8.67 |
| CUBIC trial | eth0 | 8 | 6.76 | 7.85 | 8.57 |
| Baseline | eth1 | 8 | 8.76 | 8.88 | 8.96 |
| IRQ alignment | eth1 | 8 | 8.70 | 8.87 | 8.98 |
| CUBIC trial | eth1 | 8 | 8.79 | 8.87 | 8.94 |

The alignment and offload corrections did not establish a consistent TCP
throughput improvement. They fixed the queue locality and persistence
problems. The CUBIC trial did not justify changing the saved BBR policy.
Public TCP still had substantial retransmissions; TCP samples showed zero
guest NIC, qdisc and softnet drops.

Verified simultaneous send/receive rates, with BBR at both endpoints:

| Interface | Pair | Forward Gbps | Reverse Gbps |
| --- | --- | --- | --- |
| eth0 | node1 → node2 | 6.54 | 6.64 |
| eth0 | node1 → node3 | 7.40 | 5.16 |
| eth0 | node2 → node3 | 7.39 | 4.29 |
| eth1 | node1 → node2 | 8.62 | 8.58 |
| eth1 | node1 → node3 | 8.34 | 7.77 |
| eth1 | node2 → node3 | 8.69 | 7.03 |

## IRQ and cache evidence

Across 50,889,863 observed queue interrupts after alignment, none occurred outside
the assigned paired CPU. All eight RX and TX queues were exercised, and final
requested and effective IRQ masks matched on all six NICs.

This establishes IRQ placement, not elimination of CPU cache invalidations.
Host-wide PMU samples before and after one matched workload did not establish
a cache-miss improvement: cache-miss ratios were approximately 43.68% and
44.64%, with differing delivered throughput. Application scheduling and
hypervisor vCPU/vhost placement remain outside these affinity guarantees.
The guest reports one shared L3 domain; that does not prove the physical
host's cache or NUMA placement.

## UDP results and additional limits

The original eight-thread UDP stress run concentrated transmit work onto
individual cached socket queues, causing heavy FQ per-flow drops. To remove
that confound, eight separate sender processes and server listeners were
pinned to CPUs 2–9, using one distinct TX queue each.

With the final 100-packet FQ limit, pinned node1 → node2 UDP delivered:

| Interface | Offered payload | Delivered payload | Reported loss |
| --- | --- | --- | --- |
| eth0, 1400-byte datagrams | ~10 Gbps | 3.19 Gbps | 67.63% |
| eth1, 8000-byte datagrams | ~10 Gbps | 5.84 Gbps | 41.67% |
| eth0, 1400-byte datagrams | 1 Gbps | 1.00 Gbps | 0.044% |
| eth1, 8000-byte datagrams | 4 Gbps | 4.00 Gbps | 0% |

The pinned private 10 Gbps run had zero guest transmit qdisc drops. Sender
guest NIC byte counters recorded about 10 GB over the complete sample window,
while the receiving guest NIC saw much less. Thus a substantial loss component
occurs before the receiving guest NIC. Hypervisor/vhost, virtual-switch,
physical-link or provider policing telemetry is needed to identify that
component; these tests do not distinguish among them.

A 1000-packet FQ per-flow trial was not retained. With pinned placement it
delivered 2.98 Gbps on eth0 and 5.73 Gbps on eth1, versus 3.19 and 5.84 at 100.
Increasing queue depth did not resolve the sustained-load loss. All trial
limits were restored to 100, and the old eth1 flow-limit override was disabled.
The very large UDP stress drop counters remain as historical counters, not
as an assertion that ordinary production traffic is currently dropping.

RX/TX ring sizes and queue counts are already at the guest's advertised
maximum. Configurable RSS indirection is not exposed by the hypervisor:
ethtool reports operation not supported. There is no evidence supporting a
larger guest backlog, busy polling, interrupt-delay changes or additional
software receive steering as a fix for the remaining 10 Gbps gap.

Private DF jumbo-frame probes passed for node1 → node2 → node3 → node1 at
9000-byte IP size. Public peer DF pings did not receive replies; peer routing
and device-bound ICMP return traffic make them unsuitable as public MTU
evidence here. The device-bound TCP/UDP tests, socket addresses, negotiated
MSS and interface counters were used for the public data path.

## Final validation and cleanup

All six NICs pass the read-only validator against live state and saved
NetworkManager settings. Both retired services are disabled, the IRQ service
is enabled and active, and no NetworkManager dispatcher hook exists.

Temporary benchmark listeners, firewall rules and cleanup timers were removed.
eth0 reverse-path filtering was restored to its original strict value of 1.
Temporary privileged diagnostic pods were removed after collecting final
evidence. All workers and their Cilium agents are Ready.

Original scripts, units, live masks and saved profiles are backed up on each
worker in /var/tmp/bagelbot-netqueues-20261109/. The original IRQ script has the
stale single-queue assumption; the before-runtime.json snapshot records the
actual working pre-change multiqueue masks.

Machine-readable results are in
[the measurement summary](2026-11-09-node-network-queues.json).
Raw outputs, counter snapshots and benchmark scripts are retained locally in
/private/tmp/bagelbot-netqueues-20261109/.

The kernel's [network scaling documentation](https://www.kernel.org/doc/html/latest/networking/scaling.html)
explains the IRQ/RPS/XPS locality tradeoffs. Cilium's
[performance tuning guide](https://docs.cilium.io/en/stable/operations/performance/tuning/)
describes BIG TCP ownership and limits.
