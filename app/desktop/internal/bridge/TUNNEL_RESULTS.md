# Existing Cloudflare Tunnel validation

Completed 2026-11-08. The real desktop supervisor and separately signed macOS
sandbox worker connected through the existing public Cloudflare Tunnel to an
isolated synthetic Fiber TLS fixture. TLS/system-root verification, WebSocket
and protobuf parsing ran inside the worker; the DPoP private key stayed in the
parent. No production account credentials or event data were used.

| Check | Result |
| --- | --- |
| Public HTTPS trust and Cloudflare edge | Verified system trust chain and `Cf-Ray` |
| Invalid bearer upgrade | Rejected before creating a session |
| Binary protobuf events | Sequences 1–8, exact typed payloads, no gaps or duplicates |
| Server-directed handoffs | 2; candidate bound before promotion; old socket closed after cutover |
| Socket attempts | 3 logical stream attempts, all expected; no unexpected reconnect |
| DPoP | Pinned installation key, access-token hash, route/method checks, nonce and replay checks |
| Expired server nonce | Renewed during the second handoff after the idle interval |
| Heartbeat-only interval | 150 seconds on the same socket; server keepalive every 10 seconds |
| Total runtime | 150.8 seconds; Go test completed in 151.49 seconds |
| Worker resource sample | 28,624 KiB RSS; CPU rounded to 0.0% in one coarse idle observation |
| Network-policy verification | All Kubernetes/Cilium policy specifications match the pre-test SHA-256 baseline |
| Cleanup | Temporary Job, Pod, Secret, ClusterIP Service, ServersTransport and IngressRoute removed |

The route used `health.itsbagelbot.com` and only the previously unused exact
`/bridge/v1/events`, `/bridge/v1/bind`, `/bridge/v1/migration` and `/healthz` paths.
Existing health routes were preserved. Unknown subdomains redirect to `/404` at
the Cloudflare edge, so the test reused this existing hostname without changing
that rule. No DNS, Tunnel, redirect, firewall, network-policy, NodePort or public
origin changes were made. The test did not provision a Worker, Durable Object,
Queue or a direct origin connection.

The existing Tunnel origin hop retains its pinned, verified TLS configuration.
Traefik used verified TLS with a short-lived, fixture-specific CA to the private
backend. The fixture had no service-account token, ran as non-root with dropped
capabilities and a read-only root filesystem, and had a bounded lifetime. Only
synthetic credentials were mounted; credentials, certificates and private keys
are excluded from these retained results.

Raw sanitized observations are in
[the sandboxed client/server log](compare/results/tunnel-sandbox-dpop-protobuf.txt).
The earlier direct-client run is retained in
[the historical log](compare/results/tunnel-websocket-protobuf.txt).
Logical stream attempts exclude nonce-challenge HTTP retries. The resource
sample is not a throughput benchmark or an incremental private-memory measure.
The client exercises `TestExistingTunnelWebSocketProtobuf`; the temporary server
runs `TestTunnelProbeServer`. They are opt-in tests and skip during normal runs.
With a freshly provisioned isolated fixture on those exact routes, run from
`app/desktop`:

```sh
BAGEL_TUNNEL_CLIENT=1 \
  BAGEL_TUNNEL_ENDPOINT=https://health.itsbagelbot.com \
  BAGEL_TUNNEL_TOKEN_FILE=/path/to/owner-only-disposable-token \
  BAGEL_TUNNEL_PROOF_KEY_FILE=/path/to/owner-only-disposable-pkcs8-key \
  BAGEL_TEST_SANDBOX_WORKER=/absolute/path/to/BagelNetworkWorker.app/Contents/MacOS/BagelNetworkWorker \
  go test ./internal/bridge -run '^TestExistingTunnelWebSocketProtobuf$' \
  -count=1 -v -timeout=6m
```

The fixture is no longer deployed; the command requires provisioning it again.
Its server requires `BAGEL_TUNNEL_SERVER=1`, `BAGEL_TUNNEL_HOST`,
`BAGEL_TUNNEL_TOKEN_FILE`, `BAGEL_TUNNEL_TLS_CERT_FILE`, and
`BAGEL_TUNNEL_TLS_KEY_FILE`, and `BAGEL_TUNNEL_DPOP_JKT` matching the disposable
client key. Local Fiber/TLS tests verify the same adapter without
cluster access. The test-only adapter owns request storage and restores HTTP 101
flushing around fasthttp's native HTTP adaptor; it is not shipped in the desktop
binary or offered as a production bridge server.

This proves the tested wire path and two application-directed handoffs through
the current Tunnel. It is not a long-duration availability guarantee, a load
benchmark, or an edge-restart experiment. Production ingress processes were not
restarted. Physical sockets may still close; the app handles bounded recovery,
and lossless replay requires the server's retention/sequence contract. Native
desktop account pairing now has its own client and native auth service seam;
this synthetic fixture does not validate that issuer through the Tunnel. The
production protobuf event server, deployment configuration and consumer wiring
remain separate work. macOS distribution signing/notarization and native
Windows/Linux sandbox validation remain release gates.

## Local checks and allocation scope

The following passed on Go 1.27.1, darwin/arm64:

```sh
# Repository root
go test -race ./pkg/protobuf/... ./pkg/websocket/... ./pkg/grpc/...
go vet ./pkg/protobuf/... ./pkg/websocket/... ./pkg/grpc/...

# app/desktop
go test -race ./...
go vet ./...
```

Handwritten new bridge and wrapper functions, including the live probe helpers,
pass `gocyclo -over 10`. Import guards enforce shared transport/serialization
ownership and prohibit production fixture imports. The desktop production
dependency graph includes `pkg/websocket` and `pkg/protobuf`, with no gRPC runtime.

Three warmed runs on Apple M1 Pro measured:

| Path | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Typed supervisor dispatch | 459.6–465.4 | 0 | 0 |
| Shared protobuf marshal into reused buffer | 32.24–32.84 | 0 | 0 |
| Shared owned-string protobuf decode fixture | 74.45–77.83 | 16 | 2 |

Dispatch includes validation and the consumer, but excludes wire decoding and
TLS/WebSocket reception. Payload validation and serialization scale with bounded
message bytes; queue push/pop and state dispatch have fixed work. Full network
receive is not allocation-free. These small local measurements do not describe
Cloudflare latency or server throughput.
