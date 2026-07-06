---
title: Console
description: SvelteKit application for the broadcaster dashboard and admin interface.
---

The Console (`console/`) is a SvelteKit (SSR) application that serves as the primary user interface for both broadcasters and system operators.

## Architecture

- **Broadcaster Dashboard**: Provides self-serve management for custom commands, module toggles, and notification viewing.
- **Admin Operator Interface**: Provides elevated views and controls for system operators (e.g., shard fleet monitoring, global user management, system-wide announcements).
- **Authentication**: End users authenticate via Twitch OAuth (using the `arctic` library). The grants are verified and persisted by the [Users](/microservices/users/) service via `grant_save`.
- **Communication**: The Console does not connect to the database. It talks exclusively to the backend Go services over NATS RPC via an internal adapter.
- **Exposure**: Hosted behind a `cloudflared` tunnel, exposing it securely to the public internet for end-users, while the admin sections are strictly gatekept.
