---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Controls gateway
description: "Accepts desktop controls WebSocket connections at GET /v1/connect."
docType: reference
---

## Responsibilities

It verifies signed access tokens issued by Users, relays device events and commands over core NATS, and closes connections on revocation or takeover. Device identity comes from the verified token. It does not issue tokens or run Twitch logic.

## Interfaces

The WebSocket endpoint is `GET /v1/connect`. Core NATS carries device events on `controls.device.<device_id>.evt`, commands on `.cmd`, takeover on `.takeover`, and revocation on `controls.revoked.<device_id>`.

The gateway verifies the signature, issuer, audience and expiry before using device identity. Core NATS routes device events and commands across instances. A newer connection publishes a takeover for the same device; a revocation closes its connection. Users owns signing and device grants, not this gateway.

## State and dependencies

Users signs tokens and owns grants. Core NATS routes device messages and connection lifecycle events across gateway instances.

## Failure behavior

Invalid, expired or unverifiable tokens are rejected. A takeover or revocation closes the affected connection.

## Code map

Source: [app/controls/gateway/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/controls/gateway). See `lib/controls_gateway/ws_handler.ex`, `token.ex`, `nats.ex`, and `subjects.ex`; tests are in `test/`.

## Validation

From `app/controls/gateway/`, run `mix test`. See `lib/controls_gateway/ws_handler.ex` and `token.ex`.
