---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Commands
description: "Owns custom commands, aliases, use totals, external fetch definitions and sealed keys."
docType: reference
---

## Responsibilities

Sesame reads command settings through projections or owner RPC. Gossip resolves fetch definitions and keys when needed; projected configuration must not expose decrypted credentials. Changes emit command events for Projector.

## Interfaces

The default RPC prefix is `rpc.commands`. Ordinary upserts are coalesced, so success means acceptance rather than immediate persistence; rename and delete persist immediately. A durable consumer records `data.commands.used`, committing its batch receipt and increment together before acknowledgement. Account deletion removes owned commands and fetch credentials subject to the deletion cutoff.

## State and dependencies

Commands owns its schema; Projector and Gossip consume its views and internal RPCs.

## Failure behavior

An accepted coalesced upsert is not yet a committed change. Use-count receipt and increment commit together.

## Code map

Source: [app/db/commands/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/commands). Start at `main.go`, `rpc/`, `repository/`, and `ent/schema/`.

## Validation

Run `go test ./app/db/commands/...` from the repository root.
