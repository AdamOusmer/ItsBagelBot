---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord engine
description: "Turns Discord events and selected Twitch facts into community behavior and described Discord actions."
docType: reference
---

## Responsibilities

Ingress receives events and Outgress executes REST effects. Engine resolves guild configuration through Discord data, applies module gates, and uses Outgress RPCs for synchronous orchestration. Its modules include live posts, tickets, voice rooms, roles, logs and link guard.

## Interfaces

Inputs include `discord.ingress.event.{message,member,voice,interaction,audit,guild}` and selected Twitch facts. Outputs are typed Discord commands or private Outgress RPCs.

Engine consumes the six Discord event subjects, deduplicates by message UUID and resolves the guild binding, broadcaster access and per-guild modules before dispatch. Duplicate events are acknowledged; a dedup store failure fails open with a warning. Disabled or unbound guilds produce no ordinary work. Twitch facts can fan out to multiple guilds bound to one broadcaster.

## State and dependencies

Discord data owns durable guild settings; Valkey holds ephemeral state and caches.

## Failure behavior

Duplicates are acknowledged. Deduplication store errors fail open with a warning; unbound or disabled guilds produce no ordinary work.

## Code map

Source: [app/discord/engine/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/engine). See `main.go`, `modules/all.go`, `internal/dispatch/`, and the contracts in `internal/domain/discord/`.

## Validation

Run `go test ./app/discord/engine/...` from the repository root. See `internal/dispatch/` and `modules/all.go`.
