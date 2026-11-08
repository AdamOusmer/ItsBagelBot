# Shared gRPC client

`pkg/grpc` is the retained reusable native gRPC wrapper. It owns the grpc-go
channel, TLS configuration, metadata, message codec, invocation, and server-stream
lifetime. Callers that need native gRPC use this API instead of importing grpc-go. It contains no account/session manager, reconnect
loop, migration logic, application cursor, endpoint discovery, or UI dependency.

The client explicitly selects the safe VT protobuf codec on its own calls. Its
wire content subtype is standard `proto`, so it interoperates with a normal
protobuf gRPC server. There is no JSON/BytesValue tunnel, fallback encoder, unsafe
string aliasing, or global codec registration/replacement.

`Message` and `Empty` delegate to the shared `pkg/protobuf` serialization
contract. Messages implement its four generated safe VT methods, and `Empty`
implements the protobuf empty message. Receive destinations must start
empty or contain exclusively VT-owned nested messages. `ResetVT` returns those
nested messages to their pools before decoding. Do not pass destinations filled
by a different decoder. Safe decoding owns its string and byte fields; consumers
must finish with a message before the caller reuses/resets it.

```go
client, err := grpc.NewClient(grpc.Config{
    Endpoint:        "https://api.example.com",
    MaxMessageBytes: 64 << 10,
})
if err != nil {
    return err
}
defer client.Close()

// Obtain the token through a native credential owner. Never frontend input.
auth := grpc.Credentials{Token: token}
stream, err := client.Listen(ctx, "/example.v1.Service/Listen", auth, &grpc.Empty{})
if err != nil {
    return err
}
defer stream.Close()
// Repeatedly call stream.Recv(ctx, &generatedMessage).
```

`NewClient` validates configuration and creates a **lazy** channel; success does
not prove server connectivity or authentication. The caller supplies bounded
attempt contexts and decides whether and when to retry. Automatic RPC retries
and remotely supplied service configuration are disabled. grpc-go still owns
its HTTP/2 channel connectivity mechanics.

Only HTTPS authority endpoints are accepted: no path, userinfo, query, fragment,
resolver targets, or plaintext fallback. TLS verification is required, custom
ServerName must match the endpoint, and the TLS floor is 1.2. Explicit RootCAs
support a controlled trust store. Caller TLS settings are cloned. Server response
headers are bounded to 16 KiB. Send and receive limits are configured per client,
up to the codec's 4 MiB hard ceiling. Compression is not enabled.

Each call receives explicit `Credentials{Token}`. The bearer token is placed only
in gRPC authorization metadata; the wrapper replaces inherited outgoing metadata
so stale account credentials, cookies, or arbitrary application fields cannot
ride along. Generic requests carry application-specific fields in their typed
protobuf contracts. Validation errors contain no endpoint, method, or token
values.

`Listen` sends one typed request and closes the request direction immediately.
Its returned `Stream` exposes receive and close only. Closing that stream cancels
only its RPC, while `Client.Close` terminates the owned shared channel and all its
RPCs. Overlapping streams and unary calls can reuse the same healthy HTTP/2
connection. A replacement physical connection still requires TLS establishment.
A different per-receive cancellation context is supported; the common owning
context avoids registering another cancellation callback for every receive.

Errors distinguish local known limits (`ErrTooLarge`), RPC-level resource
exhaustion (`ErrResourceExhausted`), authentication rejection
(`ErrAuthentication`), invalid endpoints (`ErrEndpoint`) and credentials
(`ErrCredentials`). Non-RPC edge responses such as HTML challenges and login
redirects return `ErrProxyResponse`; they must not clear native credentials or
be treated as an account rejection. Authentication errors require a gRPC
content-type in initial/trailing metadata. Redirects are not followed. Successful
stream receives do not inspect metadata or register extra callbacks.
A server's ResourceExhausted status can mean quota or
backpressure, so it is not guessed to be a size error. Context cancellation,
deadlines, and terminal `io.EOF` preserve their Go error identities.

The fixed message bounds and pooled codec do not imply zero allocations for the
complete TLS/HTTP2/gRPC stack. Benchmark codec and network costs separately.

```sh
go test -race ./pkg/grpc
go vet ./pkg/grpc
go test ./pkg/grpc -run '^$' -bench . -benchmem
```

Tests use a real native TLS gRPC server with the default protobuf codec to verify
standard wire interoperability, shared channel reuse, independent stream
cancellation, complete owner shutdown, authorization isolation, verified TLS,
message/header bounds, EOF, and error classification.

## Allocation measurements

On Apple M1 Pro, Go 1.27.1, three isolated codec runs measured this small typed
fixture (sequence, label, nested child, bytes payload):

| Operation | ns/op range | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Marshal to gRPC buffers | 154.5–158.1 | 16 | 1 |
| Safe unmarshal, reused pooled destination | 79.3–79.7 | 16 | 2 |

The remaining marshal allocation is the returned gRPC buffer slice. Safe decoded
strings are owned copies. Empty request/reply encoding and decoding have a
zero-allocation regression test. These are codec measurements, not full network
or RPC benchmarks; the transport comparison remains under
`app/desktop/internal/bridge/compare/OPTIMIZED_RESULTS.md`.

Small outgoing messages acquire a 4-KiB pooled backing buffer and expose only
the exact encoded length. This stays above grpc-go's small-buffer threshold so
`Free` actually returns storage to the pool. Before that change, the same
marshal benchmark used 320 B and 4 allocations per message. Fragmented receive
materialization follows the same ownership rule; a single receive buffer uses
the existing refcounted fast path. Pool misses and GC can allocate again.

## Dependency ownership

The repository import guard confines grpc-go to this package and protobuf
implementations to their shared transport/serialization owners, including
`pkg/protobuf`. Generated protobuf files are exempt, as are
raw implementation benchmarks in the exact desktop comparison-test directory.
Those exceptions do not extend to handwritten production code.

Pinned runtime dependencies are grpc-go v1.83.2 (Apache-2.0), protobuf-go v1.36.12
(BSD-3-Clause), and vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10
(BSD-3-Clause). They allow commercial use under their respective license and
notice requirements. The fixture generation instructions pin the separate
code generators in [internal/testpb](internal/testpb/README.md).

The desktop app bridge currently uses `pkg/websocket` for receive-only binary
WebSockets and same-origin HTTPS controls, with typed serialization in
`pkg/protobuf`. Its current public ingress uses Cloudflare Tunnel, which does
not provide the native gRPC public-hostname path required by this wrapper.
This package remains available for deployments that support native gRPC rather
than silently changing transport or tunneling JSON.

`pkg/grpc/testkit` retains native TLS and terminating HTTP/2 fixtures for shared
client interoperability tests and transport comparisons. Production files cannot
import those fixtures. Tests cover real GOAWAY drain across physical connections
and stream/unary multiplexing through two verified TLS legs. These fixtures do
not emulate or certify a particular Cloudflare deployment; see the desktop
README for the selected app transport and ingress constraints.
