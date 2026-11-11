---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Notifications
description: "Owns account-facing notifications and their cleanup."
docType: reference
---

## Responsibilities

RPCs serve dashboard notifications and administrative announcement operations. Other services request notifications through this owner rather than writing its records directly.

## Interfaces

User operations use `rpc.notifications.*`; administrative operations use `rpc.admin.notifications.*`.

`rpc.notifications.*` lists and marks notifications; `rpc.admin.notifications.*` sends, lists and deletes them. Request IDs make repeated creates return the existing row. Broadcast and per-user expiry have distinct roles; marking one user’s notification read does not delete the broadcast. Cleanup uses an internal RPC.

## State and dependencies

The service owns notifications and per-user receipt state.

## Failure behavior

Create requests with the same request ID return the existing row. Expiry and cleanup are separate from read receipts.

## Code map

Source: [app/db/notifications/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/notifications). See `main.go`, `rpc/`, `repository/`, and `ent/schema/`.

## Validation

Run `go test ./app/db/notifications/...` from the repository root.
