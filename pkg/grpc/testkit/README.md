# Native RPC test fixtures

This helper package exists for tests that exercise `pkg/grpc` through real TLS
and native `grpc.Server.Serve`, without letting application tests import vendor
RPC implementations. It is not a production server API.

`New(t, Service)` supplies one verified loopback TLS authority, client trust
configuration, typed unary methods, and typed server-streaming methods. Request
factories and handlers use only shared `grpc.Message`; `Call` exposes the native
bearer token and a copy of incoming metadata for assertions. A streaming handler
receives a typed send closure and a cancellation context. It must honor that
context while waiting for test events.

The fixture uses the normal protobuf server codec to verify standard-wire
interoperability with our explicitly selected safe VT client codec. Shared
`grpc.Empty` maps to standard protobuf Empty inside the fixture. No global codec
replacement or JSON tunnel is used.

`PhysicalConnections()` counts accepted TCP connections. `Stop()` force-closes
RPCs. `Drain(ctx)` starts native graceful drain, waiting until existing calls
finish; deadline expiration force-stops the fixture. `Replace(t, Service)` starts
native graceful drain/GOAWAY, waits for the listener to release, then opens the
same authority with the same certificate and a replacement service. Existing
streams remain on the old server while new calls move to the new connection.
Each fixture is stopped through test cleanup.

The replacement test verifies old delivery continues during drain, a new stream
opens on the replacement, closing the old stream lets drain finish, and the new
stream continues independently with one physical connection per server.
