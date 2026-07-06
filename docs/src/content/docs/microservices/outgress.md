---
title: Outgress
description: Manages outbound messaging to Twitch and per-broadcaster rate limiting.
---

The Outgress service (`app/outgress/`) is the bottleneck for all outbound communication to Twitch. It ensures that ItsBagelBot adheres to Twitch's strict rate limits and manages the token lifecycle for outbound requests.

## Architecture

Outgress consumes action payloads from NATS (e.g., `twitch.outgress.premium` and `twitch.outgress.standard` emitted by the [Sesame](/microservices/sesame/) pipeline). It serves as a unified gateway for translating internal module outputs into actual API requests or IRC messages.

### Rate Limiting

Twitch enforces rate limits on a per-broadcaster basis (or bot account basis). Outgress relies on **Valkey** to track these rate limits distributed across its instances. Before a message is dispatched to Twitch, Outgress asserts its quota in Valkey; if the quota is exhausted, the message is delayed or dropped depending on the lane priority.

### Token Lifecycle

The Outgress service requires access to valid Twitch OAuth tokens to interact with the API. It leverages the internal `bagel.rpc.internal.tokens.*` NATS RPC to securely retrieve tokens from the [Users](/microservices/users/) service.

### Security

Because Outgress possesses the ability to send messages as the bot or broadcaster, it is restricted. A **kill switch** mechanism allows operators to immediately sever outbound communication in emergencies.
