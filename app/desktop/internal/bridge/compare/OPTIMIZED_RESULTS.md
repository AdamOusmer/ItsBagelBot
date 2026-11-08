# Native gRPC and optimized protobuf: historical comparison

Date: 2026-11-08. These local benchmark measurements are retained unchanged.
The initial recommendation below favored native gRPC + safe vtprotobuf for the
then-requested receive-only RPC/channel-reuse design. The app's current choice is
**binary WebSockets + typed protobuf with safe vtprotobuf**, because its existing
public-hostname Cloudflare Tunnel route does not support gRPC. This deployment
constraint does not reverse the measured native gRPC advantages under saturated
small-event traffic. WebSockets used less idle memory and less CPU at
sparse/periodic rates in these trials. Neither transport wins every workload.
[Cloudflare Tunnel gRPC topology limitation](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/use-cases/grpc/)

## Correction to the earlier baseline

The first gRPC harness used grpc.Server.ServeHTTP behind net/http HTTP/2. That adapter is a different server path from native grpc.Server.Serve. The earlier conclusion that gRPC was slower under saturated load was premature: it characterized the adapter, not native gRPC. The old measurements remain in RESULTS.md for transparency and are superseded for native gRPC performance comparisons by this report.

The new harness keeps the legacy adapter mode and adds a separate native grpc.Server.Serve TLS listener using the exact same test certificate. It adds optimized generated protobuf to both transports so the result cannot be attributed only to encoding. Native standard gRPC alone accounts for most of the change; VT provides an additional improvement.

## What was measured

- Same host as the baseline: Apple M1 Pro / 16 GiB, macOS 27.0.1, Go 1.27.1, default GOMAXPROCS=10. Verified loopback TLS 1.3; no compression, no insecure TLS bypass, no production endpoints. Server process is separate from the measured client process.
- Three paired saturated runs in forward/reverse/forward order; all numbers below are medians unless a trial range is shown. CPU is client RUSAGE_SELF user+kernel CPU; wall throughput also depends on child server encoding and I/O.
- Identical typed chat, large chat and alert semantics. Generated Event/ChatEvent/AlertEvent messages; **no JSON inside protobuf**. Every delivered field and advancing sequence is checked, with exact terminal EOF/close checks.
- grpc-go v1.83.2; protobuf-go v1.36.12; protoc-gen-go-vtproto generator v0.6.0; vtprotobuf runtime selected by Go MVS at v0.6.1-0.20240319094008-0393e58bdf10. coder/websocket v1.8.15.
- VT uses generated safe/copying UnmarshalVT, ResetVT nested-message reuse and pooled codec buffers. The unmarshal_unsafe option is not generated. Mixed-kind resets, unknown-field clearing, exact-sized encoding and receive-buffer mutation ownership tests pass. The VT codec is explicitly selected for benchmark calls and does not replace the standard proto codec globally.
- Standalone protobuf payload sizes remain 94 B small chat, 4,983 B large chat and 45 B alert, for standard and VT alike. The JSON baselines are 201 B, 5,088 B and 130 B respectively.

## Saturated TLS receive and full consumption

| Fixture / mode | Wall µs/event | Client CPU µs/event [range] | Client B/event | Allocs/event | TLS read+write B/event |
| --- | ---: | ---: | ---: | ---: | ---: |
| chat / ws-protobuf | 5.782 | 4.720 [4.699–4.752] | 504 | 13 | 118.00 |
| chat / ws-vtprotobuf | 5.631 | 4.435 [4.408–4.453] | 392 | 12 | 118.00 |
| chat / grpc-native-protobuf | 0.918 | 1.780 [1.776–1.808] | 476 | 9 | 108.66 |
| chat / grpc-native-vtprotobuf | 0.797 | 1.549 [1.536–1.553] | 357 | 8 | 108.60 |
| chat_large / ws-protobuf | 12.956 | 12.424 [11.947–12.526] | 5832 | 13 | 5031.00 |
| chat_large / ws-vtprotobuf | 12.316 | 11.398 [11.388–11.604] | 5723 | 12 | 5031.00 |
| chat_large / grpc-native-protobuf | 4.771 | 14.098 [14.052–17.122] | 6470 | 6 | 5006.55 |
| chat_large / grpc-native-vtprotobuf | 4.527 | 13.595 [13.287–13.646] | 6273 | 5 | 5005.91 |
| alert / ws-protobuf | 5.668 | 4.609 [4.568–4.618] | 408 | 11 | 69.00 |
| alert / ws-vtprotobuf | 5.489 | 4.359 [4.307–4.393] | 328 | 10 | 69.00 |
| alert / grpc-native-protobuf | 0.720 | 1.371 [1.356–1.454] | 249 | 6 | 59.60 |
| alert / grpc-native-vtprotobuf | 0.714 | 1.330 [1.305–1.371] | 194 | 5 | 59.69 |

For small chat, native gRPC+VT uses about **65% less client CPU** than WS+VT (1.549 vs 4.435 µs/event) and about **7.1× greater saturated receive throughput** in this fixture. It allocates 357 B/8 allocations per event versus 392 B/12 for WS+VT. Native gRPC+VT reduces small-chat CPU by about 13% versus native standard protobuf (1.780 → 1.549 µs).

Large chat is an important exception: native gRPC+VT has higher client CPU per event (13.595 vs 11.398 µs) and more allocated bytes (6,273 vs 5,723), although it completes the saturated stream faster. Parallelism and network batching make wall time and total CPU different measures. Do not claim that every gRPC payload is cheaper.

TLS byte counts measure ciphertext on net.Conn, including record/control traffic but excluding TCP/IP headers and acknowledgements. Stream/start/terminal framing and one warm-up message are amortized into the totals. Native gRPC uses fewer encrypted stream bytes under burst traffic than the tested immediate-write WS implementation. This is consistent with different write batching/record amortization; those mechanisms were not isolated by a causal CPU profile.

## Why the server path matters

| Small chat implementation | Client CPU µs/event | Wall µs/event |
| --- | ---: | ---: |
| grpc-protobuf | 22.538 | 15.099 |
| grpc-native-protobuf | 1.780 | 0.918 |
| grpc-native-vtprotobuf | 1.549 | 0.797 |

The approximately 12.7× client CPU difference between the adapter and native standard gRPC is much larger than VT’s codec gain. The original adapter cannot be used to judge native gRPC performance.

## Paced delivery: sparse traffic is different

Three trials per rate/mode. At 100/s: 300 events/trial; at 1,000/s: 1,000 events/trial. CPU includes wake-ups, scheduler/network handling and decoding, rather than just codec work. Latency starts before server encoding/send and ends after client decode/validation. These are same-host one-way durations, not WAN RTT.

| Target rate | Mode | Client CPU µs/event | p50 µs | p95 µs | p99 µs [trial range] |
| ---: | --- | ---: | ---: | ---: | ---: |
| 100/s | ws-protobuf | 113.12 | 177 | 307 | 1869 [1472–3755] |
| 100/s | ws-vtprotobuf | 119.16 | 180 | 481 | 3391 [1959–3428] |
| 100/s | grpc-native-protobuf | 250.09 | 212 | 591 | 3617 [2993–3670] |
| 100/s | grpc-native-vtprotobuf | 255.12 | 196 | 497 | 2989 [1621–3386] |
| 1000/s | ws-protobuf | 81.55 | 104 | 352 | 1461 [1358–3244] |
| 1000/s | ws-vtprotobuf | 76.37 | 92 | 199 | 1317 [1271–2780] |
| 1000/s | grpc-native-protobuf | 169.91 | 95 | 389 | 2097 [1827–3092] |
| 1000/s | grpc-native-vtprotobuf | 174.00 | 95 | 432 | 1834 [1574–3675] |

At sparse/periodic rates, gRPC has more client CPU overhead than WS in these runs. At 100/s, WS+VT uses about 1.19% of one core while native gRPC+VT uses about 2.55%. That is about 1.36 percentage points of one core extra, not 1.36% of this entire 10-core machine. Native gRPC’s saturated advantage does not imply that its sparse-rate CPU is lower. Typical latency is close; p99 varies substantially, so there is no robust tail-latency winner from these short local runs. OS timer coalescing means rates are targets.

## Idle library footprint

Fresh client processes; retained live heap after GC compared with pre-connect GC baseline. One channel per simulated gRPC app, one WS connection per WS app. Excludes app supervisor/frame buffers/pending queue. No application heartbeat is active in this fixture; zero idle traffic does not predict production heartbeat traffic. RSS deltas are noisy page-commit measurements.

| Apps | Mode | Live heap KiB | RSS delta KiB | Extra library goroutines |
| ---: | --- | ---: | ---: | ---: |
| 1 | ws-protobuf | 26.7 | 2080 | 0 |
| 1 | ws-vtprotobuf | 26.5 | 2192 | 0 |
| 1 | grpc-native-protobuf | 217.5 | 3584 | 6 |
| 1 | grpc-native-vtprotobuf | 186.2 | 3456 | 6 |
| 20 | ws-protobuf | 367.7 | 4528 | 0 |
| 20 | ws-vtprotobuf | 369.4 | 4832 | 0 |
| 20 | grpc-native-protobuf | 1479.9 | 7248 | 120 |
| 20 | grpc-native-vtprotobuf | 1452.8 | 7408 | 120 |

Native gRPC still has the larger idle library footprint: approximately 186–218 KiB versus 27 KiB for WS in these trials, plus six library goroutines. The difference is modest in absolute desktop terms. The standard/VT idle delta can reflect pool/lazy initialization state and is not evidence of a codec optimization for idle connections. Idle CPU is tiny/noisy and no idle CPU winner is claimed.

## Native channel lifecycle

Each lifecycle includes stream/channel creation as applicable, one fully validated event, terminal EOF and cleanup. Warm cases retain the TLS channel. They do not measure complete account handoff or backend rebalance.

| Mode | Wall ms/session [range] | Client CPU ms/session | Physical TLS connections |
| --- | ---: | ---: | --- |
| cold-standard | 1.969 [1.961–1.983] | 1.043 | one per session |
| warm-standard | 0.086 [0.085–0.086] | 0.083 | one retained across all streams |
| warm-vt | 0.085 [0.084–0.101] | 0.081 | one retained across all streams |

A new warm native RPC is about 0.085 ms here, versus roughly 1.79 ms for the prior measured fresh WS/TLS lifecycle. gRPC also needs a TLS handshake when it requires a replacement physical connection. A healthy WS does not handshake per event. Channel reuse is a functional benefit, not a universal zero-handshake promise. Real TLS tests verify that cancelling one native stream leaves another stream on the same channel alive.

## Recommendation and deployment context

For a deployment that supports native gRPC, these results favored **native
grpc-go server streaming + typed protobuf with safe vtprotobuf** for one-way
application delivery, standardized RPC status/cancellation, shared unary controls
and cheap logical stream replacement. Its extra idle memory was around
160–190 KiB per connected app, while WS+protobuf used less sparse-rate CPU and
idle heap. The native server result corrected the earlier adapter-only baseline.

The app now selects **WS + binary typed protobuf with safe vtprotobuf** to retain
its existing public-hostname Cloudflare Tunnel topology. Its native event socket
exposes receive/close only; post-start account binding and migration use separate
HTTPS protobuf controls with healthy connection reuse. A supervisor owns retries,
ordered replay and primary/pending handoff. There is no gRPC runtime or
JSON-in-protobuf adapter in this app path. See [the desktop architecture](../../../README.md).

The tables above measure the isolated local comparison fixtures, not the full
supervisor or the live Tunnel path. They do not establish live compatibility or
WAN latency. **No complete network path here is zero-allocation.** The
allocation-free supervisor and reusable codec-buffer cases cover narrower
boundaries. The original native-gRPC recommendation is historical context, not a
reason to change these recorded measurements to favor the current deployment.

## Free commercial-use license check

Pinned local license files and official sources were checked. All selected candidates permit commercial use without a license fee, subject to their notice terms.

| Component | Version | License |
| --- | --- | --- |
| [grpc-go](https://github.com/grpc/grpc-go/blob/v1.83.2/LICENSE) | 1.83.2 | Apache-2.0; applicable NOTICE |
| [protobuf-go](https://github.com/protocolbuffers/protobuf-go/blob/v1.36.12/LICENSE) | 1.36.12 | BSD-3-Clause |
| [vtprotobuf runtime](https://github.com/planetscale/vtprotobuf/blob/0393e58bdf10/LICENSE) | 0.6.1 pseudo-version / 0393e58bdf10 | BSD-3-Clause |
| [vtprotobuf generator](https://github.com/planetscale/vtprotobuf/blob/v0.6.0/LICENSE) | 0.6.0 | BSD-3-Clause; embedded patterns MIT |
| [coder/websocket](https://github.com/coder/websocket/blob/v1.8.15/LICENSE.txt) | 1.8.15 | ISC |

The actual Darwin/arm64 benchmark dependency closure was inspected; no copyleft, noncommercial or unknown license was found. This is not an audit of every module required elsewhere in the monorepo or every platform. Preserve relevant copyright/license/disclaimer texts; Apache redistribution also retains applicable NOTICE and modified-file notices. Other researched libraries (fastpb/Kitex) were not benchmarked or selected here.

## Verification and scope limits

Tests cover standard/VT byte compatibility, exact buffer sizes, cross-kind pooled resets, buffer mutation ownership, advancing sequences, full field equivalence, exact EOF, native TLS stream overlap/cancellation and WS VT payloads. The decoder does not use unsafe alias mode. The complete client network/library allocation costs are reported, not hidden behind an app-only dispatcher benchmark.

Before production: test the intended proxy topology, GOAWAY and rebalance across physical connections, actual supervisor+typed RPC handoff, cursor replay after abrupt loss, pending queue saturation, sustained migration memory and other desktop platforms. Both protocols still depend on server authentication/account isolation and verified TLS. Neither prevents a privileged local process from inspecting client memory.

## Reproduce

Run timed tests sequentially, from app/desktop:

```sh
go test -race ./internal/bridge/compare
go test -c -o /tmp/bagel-transport-compare-optimized.test ./internal/bridge/compare
/tmp/bagel-transport-compare-optimized.test -test.run "^$" -test.bench "^Benchmark(NetworkReceive|NativeLifecycle)$" -test.benchtime=400ms
BAGEL_NETWORK_ORDER=reverse /tmp/bagel-transport-compare-optimized.test -test.run "^$" -test.bench "^Benchmark(NetworkReceive|NativeLifecycle)$" -test.benchtime=400ms
BAGEL_NETWORK_LATENCY=1 /tmp/bagel-transport-compare-optimized.test -test.run "^TestPacedNetworkLatency/(ws-protobuf|ws-vtprotobuf|grpc-native-protobuf|grpc-native-vtprotobuf)$" -test.v -test.count=3
BAGEL_NETWORK_LATENCY=1 BAGEL_NETWORK_PACE=100 BAGEL_NETWORK_ORDER=reverse /tmp/bagel-transport-compare-optimized.test -test.run "^TestPacedNetworkLatency/(ws-protobuf|ws-vtprotobuf|grpc-native-protobuf|grpc-native-vtprotobuf)$" -test.v -test.count=3
BAGEL_NETWORK_IDLE=1 BAGEL_NETWORK_OPTIMIZED=1 /tmp/bagel-transport-compare-optimized.test -test.run "^TestIdleFootprint$" -test.v -test.count=3
/tmp/bagel-transport-compare-optimized.test -test.run "^$" -test.bench "^BenchmarkOptimized(Encode|Decode)$" -test.benchtime=300ms -test.count=3
```

Raw logs are results/optimized-*.txt. Burst repeats are forward/reverse/forward; sparse-rate trials reverse the transport order. Generated model_vtproto.pb.go uses features marshal+unmarshal+size+pool; no unmarshal_unsafe feature. No production or database endpoints are contacted.
