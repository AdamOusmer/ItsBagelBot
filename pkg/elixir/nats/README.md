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

`Platform.Nats.Planes` provides a supervised child for each endpoint of a
shared-credential deployment. `:rpc` connects to the leaf and `:jetstream`
connects to the hub. Use `request({subject, body, opts})` for RPC and
`jetstream_request({subject, body, opts})` for JetStream API requests. Core
publish and subscribe calls take the plane explicitly. `connection/1` captures
the active connection so stream readers can monitor it and stay on the same
connection for the lifetime of a subscription or read.

`Platform.Nats.Settings` uses the connection host as its TLS server name and
keeps client certificate and key paths for reconnects. Services that use separate
identities or connection pools can build individual children through
`Platform.Nats`. `Platform.Nats.LeafFailback` moves named RPC connections back to
the node-local leaf (`NODE_NAME`, `NATS_LOCAL_LEAF_HEALTH_URL`), like
`pkg/bus/failback.go`; `Platform.Nats.HubFailback` does the same for hub
connections the caller lists, probing `nats-local` with `local_probe_settings/1`.
Both read `NATS_FAILBACK_*` through `Platform.Nats.Failback`. Service code owns
subject names, acknowledgment tracking and retry decisions.
Gnat is pinned exactly because `Platform.Nats.Wire` relies on its connection call
and header encoding.
