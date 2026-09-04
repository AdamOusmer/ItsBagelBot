---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Outgress
description: Manages outbound messaging to Twitch and per-broadcaster rate limiting.
---

The Outgress service (`app/twitch/outgress/`) is the bottleneck for all outbound communication to Twitch. It ensures that ItsBagelBot adheres to Twitch's strict rate limits and manages the token lifecycle for outbound requests.

Outgress carries no knowledge of Discord: [Dingress](/microservices/dingress/) owns that surface end to end. The one link between them is a fact, not a command -- after Helix Create Clip succeeds and the chat reply is sent, Outgress publishes a `data.twitch.clip.created` event on `BAGEL_DATA` stating what happened on Twitch; whoever cares (Dingress included) subscribes to it.

## Architecture

Outgress consumes action payloads from NATS (e.g., `twitch.outgress.premium` and `twitch.outgress.standard` emitted by the [Sesame](/microservices/sesame/) pipeline). It serves as a unified gateway for translating internal module outputs into actual API requests or IRC messages.

### Rate Limiting

Twitch enforces rate limits on a per-broadcaster basis (or bot account basis). Outgress relies on **Valkey** to track these rate limits distributed across its instances. Before a message is dispatched to Twitch, Outgress asserts its quota in Valkey; if the quota is exhausted, the message is delayed or dropped depending on the lane priority.

### Token Lifecycle

The Outgress service requires access to valid Twitch OAuth tokens to interact with the API. It leverages the internal `bagel.rpc.internal.tokens.*` NATS RPC to securely retrieve tokens from the [Users](/microservices/users/) service.

### Security

Because Outgress possesses the ability to send messages as the bot or broadcaster, it is restricted. A **kill switch** mechanism allows operators to immediately sever outbound communication in emergencies.
