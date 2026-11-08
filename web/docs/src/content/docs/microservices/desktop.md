---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Desktop app
description: Desktop client implementation and current scope.
docType: reference
---

The desktop app in `app/desktop/` is a Wails application with a Svelte frontend. Its production build shows the sign-in screen. The shell can be previewed in development with `?screen=shell`; it uses sample data. Device pairing and persisted credentials are not implemented in this client, even though the [controls gateway](/microservices/controls-gateway/) has a server-side connection contract.

## Responsibilities

`main.go` embeds the frontend build and opens the application window. The app has its own Go module so desktop dependencies stay separate from server binaries. Web links open in the system browser.

## State and dependencies

Pairing and persisted credentials are not yet implemented.

## Code map and validation

For frontend validation, run `bun run check` from `app/desktop/frontend/`. The app's [README](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/README.md) describes development and packaging commands; see [App.svelte](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/frontend/src/App.svelte) and [auth state](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/desktop/frontend/src/lib/auth.svelte.ts) for the current behavior.
