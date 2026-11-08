---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Discord ingress
description: "Owns the Discord gateway session, heartbeat, resume, presence and relay of selected events."
docType: reference
---

## Responsibilities

A Valkey lease permits one active gateway session per bot token; a standby can resume using the checkpoint. Ingress immediately defers gateway interactions. It also serves `POST /discord/interactions`, verifies signatures, acknowledges inline, and publishes the interaction for Engine. General REST effects belong to Outgress.

## Interfaces

The lease holder opens the gateway session and checkpoints session ID, resume URL and sequence. A successor can resume from that checkpoint. Gateway interaction acknowledgement is an immediate REST exception; the HTTP endpoint instead acknowledges inline after signature verification. Both paths publish an interaction for later handling, so acknowledgement is not proof of successful execution.

## State and dependencies

A Valkey lease selects the active gateway session; checkpoints support resume by a successor.

## Failure behavior

A deferred interaction has been acknowledged, not necessarily completed. Standby instances do not open another gateway session.

## Code map

Source: [app/discord/ingress/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/discord/ingress). See `main.go`, `internal/gateway/`, and `internal/relay/`; event contracts live in `internal/domain/discord/`.

## Validation

Run `go test ./app/discord/ingress/...` from the repository root. See `internal/gateway/session.go`, `lease.go` and `internal/relay/interactions.go`.
