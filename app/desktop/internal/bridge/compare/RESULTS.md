# Typed protobuf gRPC versus WebSocket: app-side experiment

Date: 2026-11-08. **Superseded for transport selection by [OPTIMIZED_RESULTS.md](OPTIMIZED_RESULTS.md). These measurements characterize the earlier HTTP adapter, not native gRPC.** Recommendation: keep WebSocket transport and adopt typed protobuf if client CPU, bandwidth, and idle footprint are the deciding factors. gRPC is a valid server-streaming API choice, but this experiment does not support calling it the slimmer implementation. The gRPC candidate uses generated typed messages, with no JSON payload wrapper.

## Environment and fairness

- Apple M1 Pro, 16 GiB RAM, macOS 27.0.1, Go 1.27.1, darwin/arm64; default GOMAXPROCS=10. Local loopback TLS 1.3, verified test certificate, compression disabled.
- grpc-go v1.83.2, protobuf v1.36.12, coder/websocket v1.8.15, repository pkg/codec/Sonic v1.15.5.
- One isolated child server process; only client-process user+kernel CPU and client-process allocations counted. Client decode uses the repository FastUnmarshal with borrowed strings for JSON and ordinary generated protobuf decode for both protobuf transports.
- Three full network runs, reversing transport order in the middle run. Tables report medians; bracketed values are the observed min/max across three runs, not confidence intervals.
- Same semantic chat, large chat, and alert values; advancing sequences, complete field comparisons, exact event count and terminal EOF/normal-close checks. Fixtures are synthetic. Logical payload sizes: chat 201 B JSON / 94 B protobuf; large chat 5,088 B / 4,983 B; alert 130 B / 45 B.
- Server generates every message, including sequence changes. Saturated wall-time throughput includes child-server marshal and I/O costs; it is not a pure client CPU measurement. Client CPU is separately sampled with RUSAGE_SELF.
- Same server TLS listener supports net/http WebSockets and grpc.Server.ServeHTTP over HTTP/2. Results apply to this fixture; a native grpc.Server.Serve deployment, proxies, WAN RTT and other platforms can behave differently.

## Saturated receive and consume

| Fixture / mode | Wall µs/event | Client CPU µs/event [range] | Client B/event | Allocs/event | TLS received B/event | TLS sent B/event |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| chat / ws-json | 6.58 | 5.400 [5.314–5.420] | 931 | 11 | 227.00 | 0.0009 |
| chat / ws-protobuf | 5.82 | 4.789 [4.725–4.818] | 504 | 13 | 118.00 | 0.0008 |
| chat / grpc-protobuf | 15.01 | 22.437 [22.007–22.581] | 411 | 9 | 134.50 | 8.5800 |
| chat_large / ws-json | 13.93 | 12.102 [12.047–12.304] | 8884 | 11 | 5136.00 | 0.0020 |
| chat_large / ws-protobuf | 12.93 | 12.246 [11.838–12.279] | 5832 | 13 | 5031.00 | 0.0019 |
| chat_large / grpc-protobuf | 34.55 | 58.797 [58.289–61.157] | 7220 | 11 | 5062.00 | 22.2100 |
| alert / ws-json | 6.29 | 5.100 [5.089–5.120] | 853 | 11 | 156.00 | 0.0009 |
| alert / ws-protobuf | 5.63 | 4.602 [4.540–4.603] | 408 | 11 | 69.00 | 0.0008 |
| alert / grpc-protobuf | 14.94 | 22.349 [22.265–22.476] | 266 | 7 | 85.55 | 8.5260 |

The small-chat CPU ratio is 4.69× gRPC/WS-protobuf in saturation. Large chat and alerts show similar direction. gRPC allocates less per small event, but more bytes per large chat. Standard end-to-end network paths are not zero-allocation in any mode. Existing allocation-free supervisor/dispatch tests cover a different boundary.

TLS counters wrap the client net.Conn below encryption. They include encrypted stream bytes and control traffic, but exclude TCP/IP headers, TCP acknowledgements and link framing. Connection/TLS setup is excluded from receive-byte totals; stream/start/terminal control and one decoder warm-up message are amortized into each event total.

## Paced chat delivery

Each 1,000/s trial has 1,000 messages; each 100/s trial has 300. Each rate/mode has three trials. Timestamp starts on the local server before encode/send and ends after client decode and payload validation; this is not RTT. CPU includes wake-ups, scheduling, decode and network handling, and initial setup effects not fully amortized away.

| Target rate | Mode | Client CPU µs/event | p50 µs | p95 µs | p99 µs [trial range] |
| ---: | --- | ---: | ---: | ---: | ---: |
| 100/s | ws-json | 96.32 | 166 | 464 | 884 [665–3116] |
| 100/s | ws-protobuf | 86.24 | 174 | 464 | 1236 [342–1766] |
| 100/s | grpc-protobuf | 202.62 | 230 | 525 | 2528 [487–6909] |
| 1000/s | ws-json | 73.95 | 88 | 194 | 1505 [689–1977] |
| 1000/s | ws-protobuf | 74.62 | 89 | 407 | 1653 [1286–2255] |
| 1000/s | grpc-protobuf | 140.52 | 91 | 352 | 1473 [721–1549] |

At 100/s the median CPU corresponds to about 0.86% of one core for WS-protobuf and 2.03% for gRPC-protobuf on this host. At 1,000/s median typical latency is almost identical (89 vs 91 µs). The tail ranges overlap substantially; do not claim a reliable p99 winner from these short runs. OS timer coalescing means these are target rates, not a guarantee of precisely spaced arrivals.

## Idle footprint

Every mode/count trial uses a fresh client process with identical linked libraries and a separate server. Delta is after establishing connections and a GC versus a pre-connect GC baseline. One channel per simulated gRPC app, one WebSocket per simulated WS app; 20-app comparison does not unfairly multiplex all gRPC apps onto one channel. RSS is noisy and includes page commitment, so live-heap deltas are the more useful comparison. These are library connection costs, excluding the production supervisor, caller-owned frame buffers and pending ring. No application heartbeat is enabled in this idle fixture.

| Apps | Mode | Retained heap KiB | RSS delta KiB | Extra library goroutines | Idle client CPU µs / second | Idle TLS bytes / second |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | ws-json | 36.5 | 2128 | 0 | 66 | 0 |
| 1 | ws-protobuf | 31.3 | 2304 | 0 | 115 | 0 |
| 1 | grpc-protobuf | 191.4 | 3520 | 6 | 102 | 0 |
| 20 | ws-json | 368.1 | 4704 | 0 | 105 | 0 |
| 20 | ws-protobuf | 373.6 | 4496 | 0 | 118 | 0 |
| 20 | grpc-protobuf | 1493.6 | 7344 | 120 | 190 | 0 |

Single-connection retained live heap was about 31 KiB WS-protobuf versus 191 KiB gRPC-protobuf (about 160 KiB extra, not an enormous absolute desktop memory cost). No idle CPU winner is established: readings are tiny and noisy. Zero idle bytes in this fixture does not predict configured production heartbeat traffic. gRPC adds about six library goroutines; application receive/supervisor goroutines would be additional for both.

## Session lifecycle and TLS reuse

Lifecycle timing includes opening a connection/RPC, receiving and validating one event, terminal framing, and cleanup. It is not solely time-to-first-event or complete account handoff latency.

| Mode | Wall ms/session [range] | Client CPU ms/session | TLS connections |
| --- | ---: | ---: | --- |
| ws-json | 1.779 [1.773–1.805] | 0.643 | one new connection per session |
| ws-protobuf | 1.789 [1.779–1.802] | 0.664 | one new connection per session |
| grpc-cold | 2.050 [2.050–2.057] | 1.097 | one new connection per session |
| grpc-warm-stream | 0.166 [0.164–0.169] | 0.183 | one for all measured RPCs |

A warm gRPC stream lifecycle was roughly 10.8× faster than a fresh WS/TLS session. A new physical gRPC channel was slightly slower than fresh WS. This verifies logical stream reuse on one physical HTTP/2/TLS connection, not seamless rollout to a different backend. A healthy existing WebSocket also does not renegotiate TLS for each message.

## Recommendation

For this desktop app’s stated CPU/bandwidth/idle-footprint priority: prefer a native read-only WebSocket adapter carrying typed protobuf. Keep the supervisor responsible for session binding, bounded retries, event sequencing and make-before-break handoff. Hiding application write capability is an API boundary; both protocols still have transport control traffic and require server-side authorization.

The measured resource comparison covers the event channel and its minimal start request, not a complete production authentication/control plane. In particular, if post-start binding and migration commands must be separate unary operations AND share the same physical connection with the read-only event stream, gRPC satisfies that requirement directly. HTTP/1.1 WebSockets would need permitted in-band control messages, a separately retained HTTPS connection, or an additional multiplexing design. Those alternatives have costs not included in the idle table. This is a real architectural reason to accept gRPC's measured overhead, independent of codec speed.

Choose typed-protobuf gRPC instead if the receive-only RPC contract, shared unary controls, standardized status/cancellation and cheap creation of additional streams outweigh its measured CPU and channel overhead. It is viable at expected desktop rates; the numbers are not a reason to describe it as inherently insecure or unusably expensive. Its small-message allocation advantage and stream-reuse advantage are real.

Do not choose gRPC solely for a supposed security upgrade or to avoid TLS renegotiation per event: neither established transport performs a handshake per event. For replacement physical connections either transport can require a handshake. Neither protects credentials from arbitrary same-user malware or a privileged debugger; security still depends on trusted server TLS identity, native credential handling and server grant/account checks.

If gRPC is selected after review, use the typed model approach from this experiment; do not ship the earlier BytesValue-wrapped JSON proposal. No production transport decision is applied by this benchmark-only change.

## Correctness and remaining readiness work

Passing checks: semantic JSON/protobuf round trips, full received values and contiguous sequences, exact stream end, two overlapping typed gRPC RPCs sharing one TLS connection, cancellation of primary RPC without terminating pending RPC, and EOF. Existing app supervisor/transport tests cover additional fake-runtime handoff/auth/deadline cases but were not rewritten as a production typed-protobuf transport here.

Before a production migration: test real end-to-end supervisor handoff across physical connections/GOAWAY, abrupt disconnect+cursor replay, buffer saturation, cancellation during account replacement, sustained migration memory bounds, intended production proxy/server topology, and WAN/Windows/Linux behavior. Payload-kind mixing and sustained load should also be added to staging tests.

## Reproduce

From app/desktop (run timed measurements sequentially, without concurrent test/benchmark processes):

```sh
go test ./internal/bridge/compare -v
go test -race ./internal/bridge/compare
go test -c -o /tmp/bagel-transport-compare.test ./internal/bridge/compare
/tmp/bagel-transport-compare.test -test.run "^$" -test.bench "^Benchmark(NetworkReceive|NetworkLifecycle)$" -test.benchtime=500ms
BAGEL_NETWORK_ORDER=reverse /tmp/bagel-transport-compare.test -test.run "^$" -test.bench "^Benchmark(NetworkReceive|NetworkLifecycle)$" -test.benchtime=500ms
BAGEL_NETWORK_LATENCY=1 /tmp/bagel-transport-compare.test -test.run "^TestPacedNetworkLatency$" -test.v -test.count=3
BAGEL_NETWORK_LATENCY=1 BAGEL_NETWORK_PACE=100 /tmp/bagel-transport-compare.test -test.run "^TestPacedNetworkLatency$" -test.v -test.count=3
BAGEL_NETWORK_IDLE=1 /tmp/bagel-transport-compare.test -test.run "^TestIdleFootprint$" -test.v -test.count=3
/tmp/bagel-transport-compare.test -test.run "^$" -test.bench "^Benchmark(Encode|Decode)$" -test.benchtime=300ms -test.count=3
```

Raw outputs are in results/; network runs use forward/reverse/forward ordering. After recording the three primary runs, the harness was mechanically split into helpers to keep every function at complexity ≤10. The measured burst loop was retained; post-refactor receive, idle, and paced smoke runs are also saved and preserve the resource-cost conclusion. The smoke runs are not included in the three-run medians above. The codec fixture schema is model.proto, compiled by protoc-gen-go v1.36.12. Benchmark fixtures use synthetic credentials and a verified local test certificate; no production service or database is contacted.
