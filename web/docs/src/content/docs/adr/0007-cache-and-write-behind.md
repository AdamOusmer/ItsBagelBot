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

We use local caches and coalesced writes for selected settings that users can resubmit. Critical state takes immediate persistence paths.

## Consequences

A process failure before a flush can lose deferred writes, and cached reads can be stale. Callers must select a path appropriate to the data’s durability and consistency needs.

## References

[Cache](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/cache), [Batch](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/batch), [Modules repository](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/modules/repository).
