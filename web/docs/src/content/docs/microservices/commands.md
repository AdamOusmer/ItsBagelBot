---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Commands
description: Manages custom chat commands, command-use totals and custom-fetch definitions.
---

The Commands service (`app/db/commands/`) owns broadcaster-defined custom chat commands, their use counts, and named custom-fetch definitions with sealed API keys.

## Architecture

This is a standard data service in the ItsBagelBot architecture:
- **Ownership**: It is the sole owner of the custom commands database schema (MySQL). No other service reads its database directly.
- **RPC Interface**: It exposes `bagel.rpc.commands.*` for the dashboard (`list`, `upsert`, `delete`, and the `fetch_*` verbs for custom fetches), and `bagel.rpc.internal.projection.commands.get` (plus `...commands.fetches.get`) for secret-free read views used by internal services.
- **Event Emission**: Ordinary command upserts are accepted at once and written in batches (a 2 second or 256 item window). Rename and delete are immediate. Committed changes are published as `data.commands.changed`, and fetch changes as `data.commands.fetch_changed`.
- **Use counts**: Sesame reports command uses on `data.commands.used`. Each batch is recorded with a receipt and its increment in one SQL transaction.
- **Cache Invalidation**: It broadcasts on `bagel.cache.invalidate.<scope>` to make the Projector and Sesame drop stale caches and re-fetch the command definitions.
- **Fetch keys**: API keys for custom fetches are encrypted at rest (`TINK_KEYSET_PATH`). Dashboard reads see metadata only; Gossip unseals a key through an internal RPC when it performs the fetch.

## Execution

The Commands service **does not** execute the commands. It only stores their definitions. Execution is handled by the [Sesame](/microservices/sesame/) core engine, which merges these custom commands into its runtime routing table alongside the baked-in commands. Gossip performs the external HTTP requests for custom fetches. Whether a channel's public commands page is shown is a Users setting and does not disable chat execution.
