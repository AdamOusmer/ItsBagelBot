---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Transactions
description: "Owns Tebex integration and webhook audits, checkout baskets, giveaway draws, winners, and award delivery."
docType: reference
---

## Responsibilities

The `/tebex` webhook and transaction RPC handlers enter this service. Users owns account eligibility and access; a promotional award and a Tebex subscription are distinct.

## Interfaces

The Tebex handler verifies a bounded request body before parsing. Accepted billing events call Users synchronously; an application failure allows Tebex to retry. A giveaway draw persists awards and outbox intent together, then a dispatcher handles grant and delivery work. Winner selection and confirmed fulfillment are separate states.

## State and dependencies

Transactions owns billing records and giveaway awards; Users owns account access and eligibility.

## Failure behavior

A failed entitlement application permits Tebex to retry. Selection does not by itself establish award fulfillment.

## Code map

Source: [app/db/transactions/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/transactions). See `main.go`, `tebex/`, `web/`, `giveaway/`, `rpc/`, and `repository/`.

## Validation

Run `go test ./app/db/transactions/...` from the repository root. See `tebex/` and `giveaway/`.
