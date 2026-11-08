---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: AutoMod
description: "Incoming chat moderation is implemented in Sesame. app/twitch/automod/ is a placeholder, not a running Rust service."
docType: reference
---

## Responsibilities

Sesame evaluates incoming chat and describes enforcement actions for Twitch Outgress. Automatic enforcement defaults off. Sesame also checks completed outgoing replies; Projector does not own an AutoMod journal API.

## Interfaces

The gate evaluates configured rules and evidence in `automod/`; `engine/moderate.go` connects decisions to the Sesame pipeline. Enforcement produces typed actions for Outgress rather than calling Twitch directly. The outgoing reply floor check is a separate pipeline concern.

## State and dependencies

The implementation runs within Sesame; the separate `app/twitch/automod/` directory is a placeholder.

## Failure behavior

Automatic enforcement defaults off; described actions still pass through Outgress.

## Code map

Source: [app/twitch/sesame/automod/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/automod). See `automod/gate.go`, `automod/config.go`, `engine/moderate.go`, and `modules/automod.go` under `app/twitch/sesame/`.

## Validation

Run `go test ./app/twitch/sesame/automod/... ./app/twitch/sesame/engine/...` from the repository root.
