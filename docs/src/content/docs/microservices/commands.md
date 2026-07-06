---
title: Commands
description: Manages custom chat commands.
---

The Commands service (`app/commands/`) is responsible for the CRUD (Create, Read, Update, Delete) operations of broadcaster-defined custom chat commands.

## Architecture

This is a standard data service in the ItsBagelBot architecture:
- **Ownership**: It is the sole owner of the custom commands database schema (MySQL). No other service reads its database directly.
- **RPC Interface**: It exposes `bagel.rpc.commands.*` for the dashboard to manage commands, and `bagel.rpc.internal.projection.commands.get` for internal services.
- **Event Emission**: When a command is created, updated, or deleted, it writes the change to the database, returns an optimistic reply to the caller, and asynchronously emits `data.commands.changed`.
- **Cache Invalidation**: It publishes to `bagel.cache.invalidate.broadcaster` to force the Projector and Sesame to drop their stale caches and re-fetch the new command definitions.

## Execution

The Commands service **does not** execute the commands. It only stores their definitions. Execution is handled by the [Sesame](/microservices/sesame/) core engine, which merges these custom commands into its runtime routing table alongside the baked-in commands.
