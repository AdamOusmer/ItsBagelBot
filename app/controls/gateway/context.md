# Controls gateway context

Agent guide for the desktop controls WebSocket gateway. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for neighboring owners.

## Purpose and boundary

The gateway is the WebSocket server the desktop app connects to at `GET /v1/connect`.
It verifies access tokens, relays device events to core NATS, and delivers
commands and revocation back to the right connection. It never issues tokens
(Users owns device grants and signing; see the device-authorization contract),
never runs Discord/Twitch logic, and never clusters with other pods: routing
across pods goes through core NATS, not BEAM distribution
(`RELEASE_DISTRIBUTION=none`).

## Local nomenclature

- **Device:** one desktop app installation, identified by `device_id` from a
  verified token, never from client input.
- **Takeover:** a newer connection for the same device displacing an older one,
  anywhere in the fleet, over `controls.device.<device_id>.takeover`.
- **Revocation:** Users ends a device grant and the gateway closes that
  device's connection, over `controls.revoked.<device_id>`.
- **conn_id:** a random id a connection generates for itself at connect time,
  used only to tell its own takeover publish apart from someone else's.

## Start with these files

| Change | Read first |
| --- | --- |
| Boot/supervision | [application.ex](lib/controls_gateway/application.ex) |
| Auth, frames, takeover/revocation/commands | [ws_handler.ex](lib/controls_gateway/ws_handler.ex) |
| Token verification | [token.ex](lib/controls_gateway/token.ex), [token/ed25519.ex](lib/controls_gateway/token/ed25519.ex) |
| NATS publish/subscribe | [nats.ex](lib/controls_gateway/nats.ex), [subjects.ex](lib/controls_gateway/subjects.ex) |
| HTTP/health/metrics | [http_server.ex](lib/controls_gateway/http_server.ex), [http_handler.ex](lib/controls_gateway/http_handler.ex) |
| Graceful shutdown | [drain.ex](lib/controls_gateway/drain.ex) |
| Effective configuration | [config.ex](lib/controls_gateway/config.ex), [runtime.exs](config/runtime.exs) |

## Token format (verification only; Users owns signing)

`v1.<base64url(payload JSON, no padding)>.<base64url(Ed25519 signature, no padding)>`.
The signed message is the ASCII bytes of `v1.<base64url(payload)>` (the first
two dot-separated segments, not the decoded payload bytes).

Payload fields: `kid` (selects the public key), `iss` (must be
`itsbagelbot-users`), `aud` (must be `controls-gateway`), `sub` (user id),
`device_id`, `grant_id`, `iat`, `exp` (unix seconds), `jti`. Unknown fields are
ignored. The gateway rejects a bad signature, wrong `iss`/`aud`, an expired
`exp`, or an unknown `kid`, and never trusts `device_id`/`sub` from anywhere
but a verified token. `CONTROLS_TOKEN_PUBLIC_KEYS` holds `kid:base64key` pairs
(comma-separated) for rotation; no private key, and no secrets, live here.

## NATS subjects (core, no JetStream)

- `controls.device.<device_id>.evt`: device to server, published with the
  server-authenticated `device_id`/`sub` stamped in, never the client's.
- `controls.device.<device_id>.cmd`: server to device, subscribed only while
  that device is connected on this pod, one subscription per connection.
- `controls.device.<device_id>.takeover`: a new connection publishes its
  `conn_id` here; any older connection for that device (any pod) that is not
  that `conn_id` closes with code 4001. Near-simultaneous double connects for
  the same device are not deduplicated (core NATS has no delivery guarantee
  across a race that tight); this is an accepted gap, not a fix target.
- `controls.revoked.<device_id>`: closes that device's connection with 4003.

## Close codes

4000 token expired (client refreshes and reconnects), 4001 takeover, 4003
revoked, 1001 going away (graceful shutdown, with jitter), 1007 malformed
frame, 1009 oversized frame (cowboy's `max_frame_size`, 64 KiB).

## Invariants and pitfalls

- Hibernate on every `websocket_handle`/`websocket_info` return: per-connection
  state must stay small, since it all survives hibernation.
- A NATS connection drop does not close sockets; each connection resubscribes
  itself once the connection is back (`Process.monitor` on the `:gnat` pid).
- Draining (SIGTERM) sets a flag that fails `/readyz` and 503s new
  `/v1/connect` attempts at the handler; it does not suspend the shared Ranch
  listener, so `/healthz` and `/readyz` stay reachable through the whole drain.
- No New Relic: that dependency is outside this service's allowed list.
  `/metrics` returns the in-process ETS counters as JSON instead.

## Local verification

Run from `app/controls/gateway`:

```sh
mix deps.get
mix format --check-formatted
mix compile --warnings-as-errors
mix test
```

Integration tests open real sockets against a real local `nats-server`
(spawned per test on a free port; install it first, e.g. `brew install
nats-server`, or see `deploy/messaging/nats-server`). `test/controls_gateway/`
covers auth rejection, expiry close, echo/ping, oversized/malformed frames,
connection isolation, takeover, revocation, command delivery, device-event
identity stamping, and drain. `test/controls_gateway/users_shared_vector_test.exs`
pins a fixed cross-service token vector shared with Users' `tokensign` tests.

`MIX_ENV=prod mix release` builds the release; the [Containerfile](Containerfile)
mirrors Twitch ingress's base images.

## Not yet wired (required before deploy)

No deploy manifest, NATS account, or permission exists for this service yet.
Before deploying: a NATS account/identity for `controls-gateway` with publish
on `controls.device.*.evt` and `controls.device.*.takeover`, subscribe on
`controls.device.*.cmd`, `controls.device.*.takeover`, and `controls.revoked.*`;
an image build/publish entry; a `deploy/k8s` manifest (HTTP port, `/healthz`
and `/readyz` probes, `RELEASE_DISTRIBUTION=none`, `ERL_FLAGS` sized from the
pod's CPU request/limit the way [twitch-ingress.yaml](../../../deploy/k8s/twitch-ingress.yaml)
does); and a Release-train entry. None of that is set up by this change.
