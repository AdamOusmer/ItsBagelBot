---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Dashboard
description: "Provides broadcaster onboarding, configuration, account management and public channel pages."
docType: reference
---

## Responsibilities

Server routes call owning services over RPC. Delegates operate permitted sections of a broadcaster’s board, and staff can inspect it through view-as. The dashboard does not execute bot commands.

## Interfaces

The session guard checks account and delegation state; `board.ts` selects the effective broadcaster for a delegate. Module and command actions validate inputs and call their data owners. Kit supplies shared catalogs and server helpers, while UI supplies reusable presentation.

## State and dependencies

Data services own configuration and account state; Kit supplies shared catalogs and helpers.

## Failure behavior

Session, account and delegation guards restrict access before protected routes act.

## Code map

Source: [web/dashboard/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/web/dashboard). See `src/hooks.server.ts`, `src/lib/server/services.ts`, `guard.ts`, and `board.ts`; shared product definitions live in `web/kit/`.

## Validation

From `web/`, run `bun run --filter dashboard check`; see `web/dashboard/package.json` for available checks.
