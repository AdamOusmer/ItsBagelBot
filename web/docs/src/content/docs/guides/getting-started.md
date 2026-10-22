---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Developer Setup
description: Architecture overview and local setup notes for contributors to ItsBagelBot.
sidebar:
  order: 1
---

Setting up the bot on your channel? Follow the [streamer guide](https://itsbagelbot.com/guides/getting-started/) instead. This page is for contributors.

Welcome to **ItsBagelBot**, a cloud-native, high-performance ecosystem designed for Twitch.

This guide covers the core concepts and steps needed to spin up the environment locally for development.

## Architecture Overview

ItsBagelBot uses a decoupled, microservice-oriented architecture:

*   **Backend Services:** Powered by **Go** and **Elixir** (the Twitch ingress).
*   **Event Driven:** **NATS JetStream** carries events and **NATS request-reply** carries RPC between services.
*   **Real-Time Communication:** Twitch events arrive over EventSub **WebSocket** shards. Tebex purchase webhooks are the one inbound webhook, and they land on the transactions service.
*   **Web apps:**
    *   **Dashboard and Admin:** SvelteKit apps in `web/dashboard` and `web/admin`, served by the cluster.
    *   **Marketing and Docs:** Astro apps in `web/marketing` and `web/docs`.
*   **Infrastructure:** Runs on **k3s** with Cilium, default-deny network policies, **Cloudflare Tunnel** for public ingress and **Tailscale** for operator access.

See the [system overview](/reference/system-overview/) and `CONTEXT-MAP.md` at the repository root for service ownership.

## Prerequisites

*   **Go** 1.26.5 (`go.mod`).
*   **Bun** 1.4.4 (`packageManager` in `web/package.json`).
*   **Elixir** 1.17 or newer, only for the Twitch ingress (`app/twitch/ingress/mix.exs`).

## Local Setup

Go services, from the repository root:

```sh
go test ./...
```

Web apps, from `web/`:

```sh
bun install --frozen-lockfile
bun run dev:dashboard
bun run dev:admin
bun run check
bun run test
```

Docs, from `web/docs/`:

```sh
bun run dev
```

Twitch ingress, from `app/twitch/ingress/`:

```sh
mix deps.get
mix test
```

Each app's `context.md` lists its focused checks.
