---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord data
description: "Owns guild bindings and settings, tickets, and member XP."
docType: reference
---

## Responsibilities

Discord Engine and Outgress use the RPC-backed store rather than opening this schema. The service also consumes user deletion events for account cleanup.

## Interfaces

The default RPC prefix is `bagel.rpc.discord-data`. `module.list` and `module.set` manage per-guild settings; Modules retains the broadcaster-level master switch. One broadcaster can bind multiple guilds, but a guild belongs to one broadcaster. `module.set` validates settings and uses an expected version for concurrency; rebinding to a different owner requires an explicit unbind.

## State and dependencies

The service owns guild configuration, bindings, tickets and XP; Engine and Outgress use its RPC adapter.

## Failure behavior

Version checks reject conflicting module writes. Account deletion is consumed for cleanup.

## Code map

Source: [app/db/discord/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/discord). See `main.go`, `rpc/`, `repository/`, `ent/schema/`, and `internal/discordstore/`.

## Validation

Run `go test ./app/db/discord/...` from the repository root.
