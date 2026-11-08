<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Controls Gateway

Elixir/Cowboy WebSocket server the desktop app connects to at `GET /v1/connect`.
Verifies access tokens, relays device events to core NATS, and delivers
commands and revocation back to the right connection. No Twitch logic
here, and no BEAM cluster: pods route to each other through NATS, not
distribution.

## What it does

- Upgrades `GET /v1/connect` to a WebSocket only after checking an
  `Authorization: Bearer` header against an Ed25519-signed access token
  (verification only; Users owns issuance). A bad signature, wrong audience,
  or expired token gets a 401 before any upgrade.
- Hibernates the connection process on every callback return to keep idle
  memory low (measured on Cowboy: about 15 KB per idle connection against
  Bandit's default). `compress: false`, a 64 KiB hard frame limit, a 25 s
  application-level ping/pong, and cowboy's own 60 s idle timeout.
- Forwards device-to-server JSON frames to `controls.device.<device_id>.evt`
  with the server-authenticated `device_id`/user id stamped in, never the
  client's. Delivers `controls.device.<device_id>.cmd` back as a text frame.
- Newest connection wins per device (`controls.device.<device_id>.takeover`);
  revocation closes a device's connection promptly
  (`controls.revoked.<device_id>`).
- SIGTERM fails `/readyz` first, then closes open sockets with a going-away
  code plus jitter so clients don't all reconnect at once.

See [context.md](context.md) for the token wire format, subjects, close codes,
and what's still required before this ships.

## Running

```sh
mix deps.get
mix test
MIX_ENV=prod mix release
```

`mix test` needs a real local `nats-server` on `PATH` (integration tests spawn
one per test on a free port). No deploy manifest, NATS account, or CI job
exists for this service yet; see context.md.
