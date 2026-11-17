---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Users
description: "Owns accounts, access status, broadcaster preferences, Twitch credentials, staff and delegation, and device grants."
docType: reference
---

## Responsibilities

Other services use its RPCs instead of reading its schema. It emits user change and deletion events for projections and downstream cleanup. Token operations have a separate internal RPC surface.

## Interfaces

Dashboard RPCs default to `rpc.dashboard.*`; device authorization uses `rpc.dashboard.device.*`. User changes and deletions publish `data.users.changed` and `data.users.deleted`.

Registration and access changes persist immediately. Preferences coalesce in a commit group and return success after MySQL commits. Committed mutations publish `data.users.changed`. Deletion writes an erasure ledger first, revokes tokens and emits `data.users.deleted`; a ledger failure prevents deletion. Sensitive token and email reads use internal RPCs, not projections. Dashboard and device operations default to `rpc.dashboard.*`.

## State and dependencies

Users owns the account and credential schema; consumers use RPC or derived projections.

## Failure behavior

Preference failures are isolated so healthy writes can commit; transient lock failures retry the whole transaction. Deletion stops if the erasure ledger write fails.

## Code map

Source: [app/db/users/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/users). Start at `main.go`, `rpc/`, `repository/`, and `ent/schema/`; device token signing is in `tokensign/`.

## Validation

Run `go test ./app/db/users/...` from the repository root.
