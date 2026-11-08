---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Developer setup
description: "Commands and starting points for contributors."
docType: how-to
---

This guide prepares contributors to run checks and a development server. For channel setup, use the [streamer guide](https://itsbagelbot.com/guides/getting-started/). These paths are independent; none starts a complete service stack.

## Prerequisites

Install the versions specified in `go.mod`, `web/package.json` and, for Twitch ingress, `app/twitch/ingress/mix.exs`. Some integration tests require NATS, Valkey, MySQL or service-specific environment variables; read the test and service `context.md` for their setup.

## 1. Check Go services

From the **repository root**, run:

```sh
go test ./...
```

The command exits successfully when the selected tests pass. Inspect skipped tests before assuming an integration path was exercised.

## 2. Check and run the web workspace

In a separate terminal, start from the **repository root**:

```sh
cd web
bun install --frozen-lockfile
bun run check
bun run test
bun run dev:dashboard
```

The checks should exit successfully. The final command stays running and prints a local address; stop it with Ctrl+C. To run the documentation site instead, start from the **repository root** in a separate terminal:

```sh
cd web/docs
bun run dev
```

The docs server prints its local address and stays running until stopped.

## 3. Check Twitch ingress

In a separate terminal, start from the **repository root**:

```sh
cd app/twitch/ingress
mix deps.get
mix test
```

The test command should exit successfully. This step is only needed when working on the Elixir ingress.

## Troubleshooting and next steps

If dependency installation fails, check the required tool versions. If an integration test cannot connect, check its backend and environment requirements; these commands do not provision them. See [testing](/guides/testing/) for focused checks and [system state](/reference/system-overview/) for service locations.
