---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Controls gateway
description: "Serves the TTS overlay browser source for OBS."
docType: reference
---

## Responsibilities

The beta serves the OBS browser source at `https://overlay.itsbagelbot.com/tts#<token>`.
The page exchanges the fragment for a signed cookie and plays audio clips in
event order. Users owns token issuance and revocation.

## Interfaces

`POST /tts/session` opens the session. `GET /tts/events` delivers clip events
and resumes after the last event ID on reconnect. `GET /tts/clip/<event_id>`
streams WAV audio scoped to the session's user. The gateway serves
`/healthz` and `/readyz`. The desktop WebSocket is unmounted.

## State and dependencies

JetStream keeps clip events and audio for five minutes. Revocation closes the
event stream. Missing or expired audio returns 404.

## Code and validation

See [app/controls/gateway](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/controls/gateway).
Run `mix deps.get` and `mix test` from that directory with `nats-server` on
your PATH.
