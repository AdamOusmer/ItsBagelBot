# Shared NATS transport

`Platform.Nats` owns connection and consumer child specifications and core publish,
request, subscribe and unsubscribe operations. Consumers use
`Platform.Nats.Server` and retain their service-specific subjects and handlers.
Request calls default to a five-second receive timeout; request and timed wire
publish timeouts must be finite integers from 1 to 60,000 milliseconds. The
timed `Platform.Nats.Wire.publish/3` path preserves the existing iodata and headers
and emits the NATS publish telemetry event without copying payloads. It accepts
only `:headers` and `:reply_to` options and applies the same subject checks as
`Gnat.pub/4`, returning `{:error, :invalid_publish}` instead of sending a
malformed command. `Platform.Nats.Probe` provides the bounded HTTP health probe.
`server_name/1` returns `nil` and `stop/1` returns `:ok` when the connection is
already gone.

Connections keep their caller-supplied TLS credentials and server identity
settings; names distinguish the independent RPC and bus planes. Service code
owns routing, failback policy, acknowledgment tracking and retry decisions.
Gnat is pinned exactly because `Platform.Nats.Wire` relies on its connection call
and header encoding.
