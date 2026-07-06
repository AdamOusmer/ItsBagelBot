---
title: Modules
description: Manages per-broadcaster feature toggles and configuration.
---

The Modules service (`app/modules/`) manages the configuration and enablement state of feature modules for each broadcaster.

## Architecture

Like other data services in the system, Modules follows a strict ownership model:
- **Ownership**: Sole owner of the modules MySQL schema.
- **RPC Interface**: Serves the dashboard settings via its NATS RPC queue, and provides internal lookups via `bagel.rpc.internal.projection.modules.get`.
- **Event Emission**: Emits `data.modules.changed` on updates.
- **Cache Invalidation**: Publishes `bagel.cache.invalidate.broadcaster` when a module is toggled or reconfigured, ensuring that the Sesame engine immediately adapts its execution pipeline.

## Module Configuration

Modules in ItsBagelBot can be configured per channel. This service stores JSON configuration blobs and enablement booleans (`IsEnabled`) that map to the module identities declared in the [Sesame](/microservices/sesame/) engine. When Sesame processes an event, it queries its local `ModuleView` projection (populated by this service) to determine if a module's handler or commands should execute.
