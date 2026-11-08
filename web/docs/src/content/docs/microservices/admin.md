---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Admin
description: "Provides the staff console for accounts, ingress, access, notifications and operational workflows."
docType: reference
---

## Responsibilities

Server actions authorize operators and call owning services. Network access and active staff authorization are separate checks; authorization failures must not be treated as success.

## Interfaces

`hooks.server.ts` establishes the session and checks staff revocation. Server access helpers recheck active staff and enforce access keys for protected actions. Giveaway operations and deploy workflows call their owners; an unavailable backing service is not evidence of success.

## State and dependencies

Users and other backing services own business state; Kit and UI provide shared code.

## Failure behavior

Authorization is rechecked server-side. A backing-service failure is not a successful operation.

## Code map

Source: [web/admin/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/admin). See `src/hooks.server.ts`, `src/lib/server/access.ts`, `services.ts`, and `admin-action.ts`.

## Validation

From `web/`, run `bun run --filter admin check` for the app’s configured checks; inspect `web/admin/package.json` for the current scripts.
