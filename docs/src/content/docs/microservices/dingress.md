---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Dingress
description: Discord gateway singleton for welcomes, voice, tickets, logs, and slash commands.
---

Dingress (`app/dingress/`) is the Discord **gateway** half of Bagel. Outgress keeps REST (go-live embeds, clip archive, 1-click guild fill, raid/gift copies). Dingress holds the one Identify session for the fleet bot token and turns gateway events into community ops.

Two Identify sessions on the same token fight. That is why the gateway is its own Deployment with **replicas: 1** and a Recreate strategy, not a replica of outgress.

## What it replaces

One Bagel bot stands in for the usual streamer Discord stack:

| Instead of | Bagel does |
|---|---|
| Welcomer | Welcome embed in `#welcome`, Member autorole, optional goodbye |
| TempVoice | Join-to-create on `+ Create voice`, cap 12 clones, `/voice name\|limit\|lock\|unlock` |
| Ticket Tool | `/ticket open\|close\|panel` and the panel button `bagel:ticket:open` |
| Sapphire (mod/logs) | `/timeout` `/kick` `/ban` `/purge` and `#logs` |
| OwO | Chat crumbs (15/msg, 60s cooldown), `/daily`, `/rank` — not hunt/zoo |
| Urchin | Already a Twitch sesame module (`!daily` and friends). Discord slash for Urchin is deferred until gossip can answer from dingress. |

Go-live, clips, raids, gifts, and milestone copies stay on the companion REST path in [Outgress](/microservices/outgress/).

## Architecture

Dingress does **not** subscribe to NATS. It:

1. Dials `wss://gateway.discord.gg/?v=10&encoding=json` with intents guilds, members, voice states, guild messages, and message content.
2. Resolves each guild through Valkey `discord:guild:{id}` (the reverse index outgress writes on setup/unbind).
3. Loads the Discord module blob from the projector store so dashboard toggles apply without an RPC hop.
4. Writes clones, tickets, and crumb counters back to Valkey (`discord:voice:`, `discord:ticket:`, `discord:xp:`).

Empty `DISCORD_BOT_TOKEN` leaves the process idle: health still serves, the gateway stays dark.

### Privileged intents

Server Members Intent and Message Content Intent must be enabled on the Discord application. Without them, welcomes and crumb ranks never fire.

### Slash catalog

Registered once on `READY` via bulk overwrite: `/ticket`, `/voice`, `/timeout`, `/kick`, `/ban`, `/purge`, `/daily`, `/rank`.

## Deploy

Doppler project `dingress-env` carries `DISCORD_BOT_TOKEN` (same value as `outgress-env`), `VALKEY_PASSWORD`, and New Relic. The bot token is the only Discord credential; it never lives in the module blob.
