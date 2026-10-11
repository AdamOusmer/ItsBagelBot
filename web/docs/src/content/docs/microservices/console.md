---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Console
description: SvelteKit application for the broadcaster dashboard and public commands pages.
---

The Console (`web/dashboard/`) is a SvelteKit (SSR) application, the broadcaster-facing dashboard. Staff operations live in the separate [Admin](/microservices/admin/) app.

## Architecture

- **Broadcaster Dashboard**: Self-serve onboarding, custom commands, modules and timers, quotes, counters, integrations (Discord, Spotify, Govee), settings, and Premium billing. Delegates can operate the sections an owner grants them, and staff can inspect a board through a short-lived view-as session.
- **Public pages**: A channel's public commands directory, plus stats, status and leaderboard hosts. A channel can hide its commands page; hiding it does not disable the commands.
- **Authentication**: End users sign in with Twitch OAuth (`oauth4webapi`). The grant is verified and persisted by the [Users](/microservices/users/) service via `grant_save`.
- **Communication**: The Console does not connect to the database. Server routes call the owning Go services over NATS RPC through shared adapters, with an in-process cache and an optional Valkey projection in front.
- **Exposure**: Routed through the `cloudflared` tunnel to Traefik on `dashboard.itsbagelbot.com` and the stats, commands and leaderboard hostnames. Only anonymous default renders are edge-cached.
