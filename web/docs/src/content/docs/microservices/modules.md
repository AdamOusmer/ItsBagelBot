---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Modules
description: Manages per-broadcaster module settings, quotes, feed tallies and integration credentials.
---

The Modules service (`app/db/modules/`) manages the configuration and enablement state of feature modules for each broadcaster. It also stores channel quote books, lifetime bagel feed tallies, and the sealed Govee and Spotify credentials.

## Architecture

Like other data services in the system, Modules follows a strict ownership model:
- **Ownership**: Sole owner of the modules MySQL schema.
- **RPC Interface**: Serves the dashboard under `bagel.rpc.modules.*` (`list`, `upsert`, `patch`, `patch-existing`, plus quote and personality verbs), and provides secret-free repair views via `bagel.rpc.internal.projection.modules.get`.
- **Event Emission**: Emits `data.modules.changed` with the persisted revision. A dashboard upsert is written before the reply returns; background writes are batched.
- **Cache Invalidation**: Broadcasts on `bagel.cache.invalidate.<scope>` when a module is toggled or reconfigured, so the Sesame engine adapts its execution pipeline.
- **Credentials**: Govee keys and Spotify app and refresh tokens are encrypted in separate tables and never enter module config blobs or projections. Gossip reads them through internal unseal RPCs.

## Module Configuration

Modules in ItsBagelBot can be configured per channel. This service stores JSON configuration blobs and enablement booleans (`IsEnabled`) that map to the module identities declared in the [Sesame](/microservices/sesame/) engine. When Sesame processes an event, it queries its local `ModuleView` projection (populated by this service) to determine if a module's handler or commands should execute.
