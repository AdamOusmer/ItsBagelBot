---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Dingress
description: Discord ingress, the gateway receiver of the Discord vertical, and how it hands off to the engine and outgress.
---

Dingress (`app/discord/ingress/`, deployed as `discord-ingress`) is the Discord **gateway** receiver. It holds the fleet bot token's Identify session, wraps selected gateway events, and publishes them to NATS. It does not decide community behavior and it does not run general Discord REST calls.

The Discord side is three services plus a data service:

| Service | Path | Role |
|---|---|---|
| Discord Ingress (this page) | `app/discord/ingress/` | Gateway session, event relay, immediate interaction defer. |
| Discord Engine | `app/discord/engine/` | Decides what to do: welcomes, tickets, voice rooms, audit logs, link guard, live and clip posts. Emits described commands; makes no Discord REST call. |
| Discord Outgress | `app/discord/outgress/` | Executes the commands over Discord REST, runs guild setup and layout, registers slash commands. |
| Discord data | `app/db/discord/` | Owns guild bindings, per-guild settings, tickets and member XP. |

## Architecture

1. Ingress dials the Discord gateway with the fleet bot token and keeps the session alive (heartbeat, Resume, reconnect).
2. For each recognized dispatch it publishes a wrapped event to one of six subjects: `discord.ingress.event.{message,member,voice,interaction,audit,guild}`. Unknown event types are dropped. Ingress only publishes; the Discord Engine provisions the `DISCORD_INGRESS` stream and binds its consumers.
3. The engine resolves the guild binding and settings (from Discord data over RPC, with Valkey caches) and publishes commands to `discord.outgress.mod` (urgent moderation) or `discord.outgress.default`.
4. Outgress consumes both lanes, moderation first, and calls Discord REST.

Two replicas run for availability, but a Valkey lease decides which one holds the gateway socket. The standby holds none and takes over from the lease holder's saved Resume checkpoint, so a rollout does not spend a fresh Identify. This is why one bot token never has two live sessions.

### Interaction defer

Ingress makes one REST call itself: for `INTERACTION_CREATE` it defers the interaction before Discord's three-second deadline. If the defer fails, the event is dropped. The engine and outgress finish the interaction later through a followup. The defer shares the fleet-wide Valkey rate budget with outgress.

### Presence

Ingress sets the bot's presence from the public users counts RPC (`NATS_INTERNAL_USERS_COUNTS_SUBJECT`, default `bagel.rpc.internal.users.counts.get`).

### Privileged intents

The gateway session requests the Guilds, Guild Members, Guild Moderation, Guild Expressions, Guild Invites, Guild Voice States, Guild Messages and Message Content intents. Server Members Intent and Message Content Intent are privileged and must be enabled on the Discord application. Without them, welcomes and crumb ranks never fire.

## Deploy

`deploy/k8s/discord.yaml` runs `discord-ingress` (2 replicas), `discord-engine` (3) and `discord-outgress` (3), all with rolling updates. Their secrets come from the Doppler project behind the `discord-env` secret. `DISCORD_BOT_TOKEN` is the only Discord credential and reaches ingress and outgress; it never lives in a module blob. An empty `DISCORD_BOT_TOKEN` leaves ingress idle: health still serves, the gateway stays dark.

The dashboard's Discord setup RPC keeps its historical `bagel.rpc.dingress` prefix, but it is served by Discord Outgress, not by this service.
