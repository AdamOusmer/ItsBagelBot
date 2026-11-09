# Shared Cowboy server

`Platform.Cowboy.Server` starts an HTTP/1-only Cowboy listener and upgrades a Plug
connection to a `Platform.Cowboy.Handler`. Listener options are `name` (required),
`plug` (required), `port`, `scheme` (`:http` or `:https`), `ip`, `num_acceptors` (default 2), `max_connections` (default 16,384, cap 1,000,000), and paired `certfile`/`keyfile` for HTTPS. Upgrade options are positive integers:
`max_frame_bytes` (default 65,536, cap 1,048,576),
`max_messages_per_second` (default 50, cap 1,000),
`max_mailbox_messages` (default 1,000, cap 10,000), and `idle_timeout`
(default 60,000 ms, cap 300,000 ms). Unknown and duplicate options are rejected.
Compression is disabled. Callback pushes accept at most 16 frames with binary payloads; each payload is bounded by the configured frame limit and ping/pong payloads by 125 bytes. Close codes and UTF-8 reasons are validated. The mailbox limit is checked when a callback runs; it
is an overload trigger, not a hard queue allocation cap. The optional
`:hibernate` result hint is passed to Cowboy.

Cowboy is pinned to upstream commit `3a630c07e1521abf63a1b9ea080509aa1337b60d`
for CVE-2026-115701. Cowlib is pinned to upstream commit
`a3b15e29b3067644bfd580246c87f7af7a90b3b8` for CVE-2026-113431; this
commit also descends from `4401b858f2229abb3ea115c60924f6c77e5f8aa3`,
which fixes CVE-2026-43966. These pins can be replaced by fixed Hex releases.

## Ingress status listener

Ingress uses the following child, with its existing environment validation and
TLS selection:

```elixir
{Platform.Cowboy.Server,
 [name: :ingress_status, plug: Ingress.StatusPlug, port: port,
  scheme: :http, num_acceptors: 2]}
```

For TLS set `scheme: :https` and pass both `certfile` and `keyfile`. The listener keeps two acceptors.
The package does not set VM flags, alter ingress event handling, or copy payloads.
TLS supports only TLS 1.2 and 1.3. Tests generate a temporary certificate with OpenSSL.
