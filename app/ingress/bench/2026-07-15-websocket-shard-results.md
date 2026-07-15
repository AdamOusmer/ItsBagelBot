# WebSocket shard capacity — 2026-07-15

The benchmark exercised a real `Ingress.ShardSession` over TLS 1.3 with
Twitch-shaped 1,338-byte `channel.chat.message` notifications. It included
Mint WebSocket framing, the shard GenServer mailbox, native OTP JSON decoding,
broadcaster extraction, rolling load accounting, and dispatcher handoff. The
downstream handler was a counter, so neither NATS nor pipeline processing was
part of this capacity measurement.

Each temporary pod used the production VM shape (`+S 2:2`, two-CPU limit), 512
dispatcher workers, a 25,000-event warm-up, and 500,000 measured events. Three
samples ran on every eligible ingress node.

| Node | Sample 1 | Sample 2 | Sample 3 | Slowest |
|---|---:|---:|---:|---:|
| node2 | 19,485/s | 19,041/s | 18,979/s | 18,979/s |
| node3 | 17,516/s | 17,815/s | 17,813/s | **17,516/s** |
| worker1 | 35,809/s | 35,689/s | 35,494/s | 35,494/s |

All nine samples dispatched all 500,000 events, ended with zero pending work,
and kept the shard mailbox at three messages or fewer.

## Capacity decision

- Per-WebSocket rated capacity: **16,000 events/s**, rounded below the slowest
  observed production-node result.
- Autoscale target at 75%: **12,000 events/s per shard**.
- Shared NATS sustained ceiling: **123,000 events/s**.
- Maximum useful automatic shard count: `ceil(123,000 / 12,000)` = **11**.

At the 12,000/s scale point, the slowest tested node retains about 31% headroom
to measured shard saturation. The benchmark runner removes every temporary pod
and never connects to Twitch or NATS.

Re-run all nodes from the repository root:

```sh
app/ingress/bench/websocket_shard_cluster.sh
```
