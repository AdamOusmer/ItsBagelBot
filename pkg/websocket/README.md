# Shared native WebSocket client

`pkg/websocket` owns verified WSS reception, the reusable HTTPS transport, bearer
headers, protocol-level framing, and connection cancellation. Application code
uses this package rather than importing a WebSocket implementation directly.
Account binding, session state, retry budgets, heartbeat deadlines, handoff, event
routing, and serialization belong to the calling app and its supervisor.

The desktop bridge currently selects binary WebSockets plus typed protobuf,
because its public ingress uses Cloudflare Tunnel. `pkg/protobuf` owns protobuf
serialization; this package treats message bodies as opaque binary bytes. It
contains no JSON envelope or protobuf schema dependency.

## API and ownership

```go
client, err := websocket.NewClient(websocket.Config{
    Endpoint:        "https://api.example.com",
    MaxMessageBytes: 64 << 10,
    Subprotocol:     "bagel.bridge.v1",
})
if err != nil {
    return err
}
defer client.Close()

// The token comes from the native credential owner, never frontend input.
conn, err := client.Dial(ctx, "/stream", token)
if err != nil {
    return err
}
defer conn.Close()

var buffer [64 << 10]byte
n, err := conn.Read(ctx, buffer[:])
if err != nil {
    return err
}
// Decode buffer[:n] using the shared serialization package, then consume it.
```

`NewClient` validates configuration without opening a connection. `Dial` performs
one authenticated upgrade. A connection exposes only `Read` and `Close`: the app
cannot send application messages on it. The implementation still handles normal
WebSocket protocol control traffic, including ping/pong and close frames.

Exactly one owner reads a connection. Each successful `Read` copies one complete
binary message into the caller's buffer. The caller owns those bytes and chooses
when to reuse them; no internally pooled payload is exposed. Text messages are
rejected. An exact-limit message is valid, but a message exceeding either the
configured bound or the supplied buffer closes that connection. The configured
limit must be between one byte and 4 MiB. The app applies its smaller protocol
limit above this generic transport.

An optional subprotocol is offered during the upgrade and must be selected
exactly by the server. Both TLS and the HTTP transport explicitly use HTTP/1.1
for upgrades, even if HTTP/2 is available or was offered in caller configuration.
Compression is disabled.

`Conn.Close` immediately releases its socket; it does not wait for an application
drain or migration handshake. The supervisor completes that work before closing.
`Client.Close` cancels its owned operations and closes all tracked connections
and idle HTTP transport connections. Connection registration/removal uses a map;
the package has no app-specific fixed slot count or reconnect policy.

## HTTPS controls

`Client.Post(ctx, path, token, contentType, body)` sends bounded bytes to the same
configured HTTPS authority and returns `(status, error)`. The caller interprets
that status; the desktop bridge expects `204 No Content`. Response bodies are
not exposed, and draining is capped at 4 KiB. Control requests reuse the client's
HTTP/1.1 keepalive transport. A WebSocket upgrade owns a separate physical
connection, so controls do not share the upgraded socket itself.

The client has no implicit operation timeout or automatic retry loop. Callers
provide cancellable, bounded attempt contexts and decide whether to retry.
Closing the client cancels outstanding operations. The native credential owner
supplies authorization independently for each dial or control request.

`DialAuthorized` and `PostAuthorized` accept project-owned
`Authorization{Scheme, Token, Proof}`. The native OAuth client uses `DPoP` with a
fresh proof produced outside this transport; a missing/invalid proof never falls
back to Bearer. The original `Dial`/`Post` methods retain explicit Bearer behavior
for existing consumers and fixtures. Neither form sends application messages on
the event socket.

Authorized replies expose only status and a bounded, unambiguous `DPoP-Nonce`.
A resource challenge requires `WWW-Authenticate: DPoP error="use_dpop_nonce"`
and a nonce on HTTP 400/401 before returning `ErrNonce`. A fresh nonce attached
to an `invalid_token` rejection remains an authentication failure. HTML edge
challenges are not nonce challenges. The caller owns a bounded fresh-proof retry;
the transport does not replay requests or interpret OAuth JSON responses.

## Transport boundary

Only HTTPS authority endpoints are accepted: no userinfo, path, query, fragment,
or plaintext fallback. Operation paths must be absolute paths on that authority;
absolute foreign URLs, authority-relative URLs, queries, and fragments are
rejected. Redirects are never followed, so a response cannot move a bearer to a
different origin.

TLS verification remains enabled. Caller settings are cloned, the minimum TLS
version is 1.2, and a custom ServerName must match the endpoint. Controlled root
CAs can be supplied for private trust stores and test fixtures. Cookies and
account tokens are not retained in a browser session or URL.

Errors distinguish origin `401`/`403` authentication rejection
(`ErrAuthentication`) from HTML or `Cf-Mitigated` edge responses
(`ErrProxyResponse`). Other upgrade failures use `ErrRejected` or `ErrDial`;
redirects use `ErrRedirect`. These classifications do not certify which remote
component generated every possible response. The app must not turn a proxy
challenge into an account sign-out. Upgrade and control errors do not expose
server reply bodies or bearer values. Size, frame-type, configuration and
closed-client errors also have static identities.

## Tests and dependency ownership

```sh
go test -race ./pkg/websocket/...
go vet ./pkg/websocket/...
```

Tests exercise real TLS binary frames, buffer ownership, exact/oversized bounds,
text rejection, HTTPS controls, cancellation, owner shutdown, authentication,
subprotocol selection, HTTP/1.1 upgrades against HTTP/2-capable servers, redirect
rejection, proxy challenges, and invalid TLS settings.

The import guard rejects handwritten raw WebSocket implementation imports outside
this package. The existing Discord gateway's exact directory is grandfathered;
no Discord code is changed. Direct vendor comparisons remain allowed only in the
exact desktop comparison test directory. `pkg/websocket/testkit` is test-only and
cannot be imported by production files, including this package's own client.
Its generic native handler supports real TLS fixtures and Fiber HTTP adapters
with Hijacker support. A fixture is not a production server or proof of a
particular Cloudflare deployment.

## Allocation budget

The receive wrapper avoids per-message `io.ReadAll` and accepts reusable caller
buffers. Binary copying and serialization still scale with payload size. TLS,
WebSocket framing, cancellation machinery, and decoding can allocate; this API
does not promise zero allocations for a complete network receive. Dialing,
connection bookkeeping, controls, and shutdown are separate from a warmed event
path. Compare the existing codec and full-network benchmarks before attributing
resource costs to an individual layer.
