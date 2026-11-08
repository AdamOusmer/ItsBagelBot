---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Shared packages
description: "Where common infrastructure behaviour lives."
docType: reference
---

## Scope

Service code uses the shared packages for infrastructure access. If a backend operation is missing, extend its shared package instead of issuing raw client commands or adding scripts in `app/`. The repository's `AGENTS.md` also requires existing builders so their timeouts, metrics and identity checks remain in place.

## Package map

| Package | Start here for |
| --- | --- |
| `pkg/bus` | NATS connections, publishing, consumers and request-reply wiring |
| `pkg/db` | Database connection, configuration, pooling and retry helpers |
| `pkg/valkey` | Valkey client operations and atomic primitives |
| `pkg/ratelimit` | Rate-limit coordination and storage |
| `pkg/cache` | Local caches, loading and invalidation helpers |
| `pkg/batch` | In-memory coalescing and flush lifecycle |
| `pkg/svcboot` | Common service startup and infrastructure wiring |
| `pkg/health` and `pkg/monitor` | Health and monitoring support |

## Failure contracts

These packages do not all have the same failure contract. For example, `pkg/batch` keeps pending updates in memory and retries failed flushes; an accepted write is not automatically durable. A NATS timeout may mean no reply was received, not that a remote operation did not run. Read the function, tests and calling service before choosing retries or reporting success.

The package tests live beside their implementations. Service-specific wiring and domain rules remain in `app/` and `internal/`; use the shared layer for backend mechanics and the owning service for business decisions.
