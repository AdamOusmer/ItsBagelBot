---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Modules
description: "Owns broadcaster module settings, quotes, feed and personality state, and integration credentials."
docType: reference
---

## Responsibilities

Dashboard writes go to the owner; Sesame reads projected settings. Gossip resolves integration credentials when needed. Module changes feed Projector; the presence of a module in code does not mean a channel enabled it.

## Interfaces

The default RPC prefix is `rpc.modules`, including `list`, `upsert`, `patch` and `patch-existing`. Projection repair uses `rpc.internal.projection.modules.get`.

Dashboard upserts persist before returning; background writes may coalesce. A patch with a stale expected revision returns a conflict. `patch-existing` also requires the expected row ID and never creates a replacement. Committed changes invalidate local caches, and the internal projection RPC returns a secret-free view.

## State and dependencies

Modules owns channel settings and integration credentials; Projector stores secret-free derived views.

## Failure behavior

A stale expected revision produces a conflict. Background writes can remain pending after acceptance.

## Code map

Source: [app/db/modules/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules). Start at `main.go`, `rpc/`, `repository/`, and `ent/schema/`.

## Validation

Run `go test ./app/db/modules/...` from the repository root.
