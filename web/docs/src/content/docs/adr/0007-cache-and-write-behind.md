---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0007 - Cache and write-behind"
description: "Architecture decision: Cache and write-behind"
docType: decision
---

**Date:** 2026-06-09

**Status:** Accepted.

## Context

Repeated settings reads and writes can put unnecessary load on the database.

## Decision

We use local caches and coalesced writes for selected settings. Command edits and user preferences now wait for their grouped MySQL transaction to commit before returning success. Modules and critical state take synchronous persistence paths. See the current [durability contract](/data-and-state/caching/).

## Consequences

A process failure before commit requires a retry; a successful response confirms committed data. Cached reads can be stale. Counter events use a 70-minute Valkey receipt fast path plus durable SQL receipts to make retries idempotent within the SQL receipt retention window. Attributable item failures are isolated from healthy writes.

## References

[Cache](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/cache), [Batch](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/batch), [Modules repository](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules/repository).
