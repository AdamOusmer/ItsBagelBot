---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Outgress
description: Manages outbound messaging to Twitch and per-broadcaster rate limiting.
---

The Outgress service (`app/twitch/outgress/`) is the bottleneck for all outbound communication to Twitch. It ensures that ItsBagelBot adheres to Twitch's strict rate limits and manages the token lifecycle for outbound requests.

Outgress carries no knowledge of Discord: the Discord services ([Dingress](/microservices/dingress/) and its engine and outgress) own that surface end to end. The one link between them is a fact, not a command. After Helix Create Clip succeeds and the chat reply is sent, Outgress publishes a `data.twitch.clip.created` event on `BAGEL_DATA` stating what happened on Twitch. Whoever cares, the Discord Engine included, subscribes to it.

## Architecture

Outgress consumes typed action messages from NATS: `twitch.outgress.premium` and `twitch.outgress.standard` (emitted by the [Sesame](/microservices/sesame/) pipeline) for viewer-facing output, and `twitch.outgress.system` for control jobs such as EventSub enrollment and stream-status checks. It translates them into Helix API calls and chat sends. It also serves management and read RPCs under `bagel.rpc.outgress.*` (channel points, stream info, chatters, trials).

Chat messages are perishable: the chat stream keeps them for five seconds, while the system stream keeps control jobs for five minutes. Outgress obtains the live Conduit ID from [Twitch Ingress](/microservices/twitch-ingress/) (`bagel.rpc.ingress.conduit.get`).

### Rate Limiting

Twitch enforces rate limits per broadcaster, per bot account and per app. Outgress relies on **Valkey** to coordinate these limits across all its instances, with separate budgets for chat and for Helix calls. Before an action is dispatched, Outgress takes quota from the shared budget. A chat message that cannot be sent before the chat stream's five-second retention ends is dropped.

### Token Lifecycle

The Outgress service requires access to valid Twitch OAuth tokens to interact with the API. It leverages the internal `bagel.rpc.internal.tokens.*` NATS RPC (`get` and `save`) to retrieve and store tokens through the [Users](/microservices/users/) service, and coordinates refreshes through a shared lease so replicas do not refresh the same token at once.

### Security

Because Outgress possesses the ability to send messages as the bot or broadcaster, it is restricted. A **pause** switch, stored durably (JetStream key-value state), lets operators stop outbound execution fleet-wide in emergencies: while paused, jobs are not executed and are retried. Output marked with a trial origin (an observed, unregistered channel) is refused before any external effect.
