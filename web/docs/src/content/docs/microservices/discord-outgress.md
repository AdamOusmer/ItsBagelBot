---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord outgress
description: "Executes Discord REST actions and owns setup and layout operations."
docType: reference
---

## Responsibilities

It consumes described actions from Engine and serves setup and engine-facing RPCs. It uses the shared Discord rate limiter and the RPC-backed Discord data store. Ingress retains the immediate interaction acknowledgement exception.

## Interfaces

Moderation actions use `discord.outgress.mod`; ordinary actions use `discord.outgress.default`. The worker polls moderation first, but a selection race can allow one ordinary action ahead of it. Malformed commands are acknowledged and dropped; handler errors receive paced redelivery with a bounded budget. Dashboard setup RPCs retain the historical `bagel.rpc.dingress` prefix; private Engine RPCs use `bagel.rpc.discord-outgress`.

## State and dependencies

The shared Discord rate limiter coordinates REST capacity; durable guild data remains with Discord data.

## Failure behavior

Malformed commands are dropped after acknowledgement. Handler failures receive paced, bounded redelivery.

## Code map

Source: [app/discord/outgress/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/outgress). See `main.go`, `internal/commands/`, `internal/setup/`, and `internal/rpc/`.

## Validation

Run `go test ./app/discord/outgress/...` from the repository root. See `internal/commands/` and `internal/rpc/`.
