---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Loyalty
description: "Owns viewer loyalty balances, watch time and channel counters."
docType: reference
---

## Responsibilities

It consumes earned and counter events and handles user lifecycle events. A watchtime consumer applies awards; RPCs expose owner operations and reads. Projector maintains derived live counter views.

## Interfaces

The default RPC prefix is `rpc.loyalty`. The service consumes earned and counter events as well as user lifecycle events.

Legacy earned events accumulate in memory, so an accepted delta may not appear in an immediate read. Counter events are acknowledged after SQL processing, with a time-limited Valkey receipt: a crash between SQL and the receipt can cause a replay to count again. Watch awards use a separate outbox; deduplication and credit commit together, and errors leave delivery pending. These paths do not share an exactly-once guarantee.

## State and dependencies

SQL owns balances and counters; Valkey supports watchtime delivery and counter receipts.

## Failure behavior

The counter SQL-to-receipt crash gap permits a replay to count again. Watch-award errors leave delivery pending.

## Code map

Source: [app/db/loyalty/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/loyalty). See `main.go`, `repository/`, `rpc/`, and `internal/watchtime/`.

## Validation

Run `go test ./app/db/loyalty/... ./internal/watchtime/...` from the repository root.
