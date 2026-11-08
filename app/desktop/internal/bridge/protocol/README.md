# Desktop bridge protobuf contract

The app uses shared `pkg/websocket` for bare native WSS/HTTPS and
`pkg/protobuf` for safe optimized serialization. This package owns message types,
validation and route names; the supervisor owns account binding, liveness,
retries, migration and ordered event delivery. The schema contains no JSON,
opaque bytes, Any/Struct envelopes, credentials or RPC service definitions.

The app-side wire contract is:

- `GET /bridge/v1/events`: verified WSS, binary protobuf only, negotiated
  `bagel.bridge.v1` subprotocol. The initial upgrade carries the native account
  token in its Authorization header and no account binding/cursor payload.
- `POST /bridge/v1/bind`: native HTTPS `application/protobuf` BindAccount,
  containing the received session ID, migration reference and applied cursor.
- `POST /bridge/v1/migration`: native HTTPS `application/protobuf`
  MigrationRequest, requesting a fresh server migration reference.

Native controls require HTTP 204. That response does not authorize event
handling: the server's session_bound message on the socket is authoritative.
Tokens appear only in authenticated native request headers. There are no client
application messages on the WebSocket; WebSocket control frames are handled by
the shared transport. The shared HTTPS client reuses healthy control connections.
These routes define the client-side contract; this package adds no server.

The server must flush session_started promptly and send a Keepalive message at
least every 20 seconds when otherwise idle. The app's default stream watchdog is
45 seconds. Keepalive is server-to-app application data; the client sends no
application heartbeat on the event socket. WebSocket control traffic through a
proxy does not establish origin-stream health. A keepalive cannot replace server replay or
make an unsupported ingress topology compatible.

ServerMessage carries session_started, session_bound, reconnect, handoff_ready,
event, auth_error, and keepalive kinds. The supervisor authorizes reconnect targets
before sending credentials; field validation alone does not establish trust.
Every server message has version 1. A field mask rejects extraneous non-default
known fields for each kind. Proto3 default-valued scalars have no explicit presence.
Unknown protobuf fields are rejected on the envelope, event and payload types,
including typed controls. A new field therefore requires a coordinated schema
and validation update; it cannot become an opaque extension carried through the
sandbox. Legal protobuf duplicate scalar fields retain last-value semantics;
only the final, fully validated typed value can reach dispatch.

Events have exact uint64 sequence numbers and int64 timestamps. Chat and alert
payloads are typed and mutually exclusive: event kind must match the populated
payload. Validation bounds identifiers at 512 bytes, binding/migration windows at
1–120000 ms, target URLs at 2048 bytes, display names/alert strings at 256 bytes,
and chat text at 16 KiB; strings must be valid UTF-8. The shared client bounds
received protobuf bytes separately (the app uses 64 KiB).

All receive types and nested payloads use generated safe VT pools. Use an empty
or VT-pool-owned receive destination. `ResetVT` before reuse clears absent fields
and returns old nested objects; ordinary `UnmarshalVT` alone has merge semantics.
Safe decode copies input-backed strings, so receive-buffer release cannot corrupt
an event. To transfer an event to a queue, detach `message.Event` by setting it
nil before resetting/returning the envelope. The runtime/queue then owns the event
and returns it to its VT pool after processing. App Consumer callbacks only borrow a
read-only event during Apply: no retention, mutation, reset or pool return. No
unsafe alias decoder is enabled.

Generation used protoc v7.35.1, protoc-gen-go v1.36.12 and
protoc-gen-go-vtproto v0.6.0. Ordinary builds/tests use committed generated files.
Optional pinned plugin installation:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto@v0.6.0
```

From the repository root, with pinned plugins available to protoc:

```sh
protoc -I . --go_out=. --go_opt=paths=source_relative \
  --go-vtproto_out=. \
  --go-vtproto_opt=paths=source_relative,features=marshal+unmarshal+size+pool \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.ServerMessage \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.Event \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.ChatEvent \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.AlertEvent \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.BindAccount \
  --go-vtproto_opt=pool=ItsBagelBot/app/desktop/internal/bridge/protocol.MigrationRequest \
  app/desktop/internal/bridge/protocol/bridge.proto
```

Validation is O(1) in field count and linear in bounded text bytes. Its warmed
allocation budget is zero. Safe protobuf decoding and WebSocket transport are
measured separately and are not described as zero allocation.
