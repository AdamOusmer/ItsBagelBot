---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Gossip
description: "Provides typed RPC access to external APIs, including provider-specific caching, upstream budgets and credential lookup."
docType: reference
---

## Responsibilities

Callers request facts rather than calling providers directly. Trusted and untrusted HTTP routes have different controls; untrusted requests use guarded destination resolution and the WARP proxy route, which fails closed if unavailable.

## Interfaces

The default RPC subject prefix is `bagel.rpc.gossip`; individual provider endpoints are registered in `internal/providers/`.

Providers are registered in `internal/providers/`; each declares endpoints and trust before constructing clients. Cached absence differs from an infrastructure failure. Custom fetches revalidate destinations and apply channel, definition and host budgets. WARP listener health alone does not prove that upstream traffic succeeds; trusted providers can remain available when the untrusted route is down.

## State and dependencies

Provider caches and shared upstream budgets support typed requests. Credentials remain with their owners and are resolved as needed.

## Failure behavior

Untrusted requests fail closed when the proxy route is unavailable. Cache absence is distinct from infrastructure failure.

## Code map

Source: [app/gossip/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/gossip). See `main.go`, `internal/engine/`, `internal/providers/`, and `internal/core/http.go`. Provider configuration and the RPC prefix are in `internal/config/`.

## Validation

Run `go test ./app/gossip/...` from the repository root; `internal/providers/custom/` covers the custom-fetch path.
