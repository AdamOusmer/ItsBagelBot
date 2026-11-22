<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Controls Gateway

The Elixir/Cowboy gateway serves the OBS browser sources on
`https://overlay.itsbagelbot.com`. The TTS beta at `/tts#<token>` exchanges the
fragment token for a signed session, delivers clip notifications over SSE and
streams audio from JetStream Object Store. Users owns token issuance and
revocation. The Powered by overlay at `/powered#<token>` fetches its config
from Projector. Overlays owns its tokens.

The gateway mounts `/tts`, `/tts/session`, `/tts/events`, `/tts/clip/<event_id>`,
`/powered`, `/powered/config`, `/powered/assets/<file>`, `/healthz` and
`/readyz`. The desktop WebSocket handler is unmounted.

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
