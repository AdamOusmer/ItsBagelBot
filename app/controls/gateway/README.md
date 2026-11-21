<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Controls Gateway

The Elixir/Cowboy gateway serves the TTS beta OBS browser source at
`https://overlay.itsbagelbot.com/tts#<token>`. It exchanges the fragment token
for a signed session, delivers clip notifications over SSE and streams audio
from JetStream Object Store. Users owns token issuance and revocation.

The beta mounts `/tts`, `/tts/session`, `/tts/events`, `/tts/clip/<event_id>`,
`/healthz` and `/readyz`. The desktop WebSocket handler is unmounted.

See [context.md](context.md) for the protocol, JetStream resources and deployment
dependencies.

## Running

```sh
mix deps.get
mix test
MIX_ENV=prod mix release
```

Integration tests need `nats-server` on `PATH`. The TTS tests create their own
file-backed stream and Object Store bucket in a local JetStream server.
