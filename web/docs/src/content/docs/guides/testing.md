---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Testing
description: "Choose focused checks and find integration requirements."
docType: how-to
---

## Prerequisites and scope

Run checks from the repository root unless a command says otherwise. Start with the package you changed, then expand to its callers and related contracts.

## Run Go checks

```sh
go test ./app/projector/...
go test ./internal/projection/...
go test ./...
```

The Go CI workflow (`.github/workflows/main.yml`) runs package tests with the race detector in shards and runs the build guard uncached. Use `go test -race ./path/to/package/...` for a focused concurrency check. Some integration tests need external services or explicit environment settings; read the test and the service's `context.md` before assuming a skipped test exercised a backend.

## Run web and ingress checks

For web changes:

```sh
cd web
bun install --frozen-lockfile
bun run check
bun run test
```

For documentation, run `bun run build` from `web/docs/`. For Twitch ingress, run `mix deps.get` and `mix test` from `app/twitch/ingress/`. CI runs ingress tests with `MIX_ENV=test` (`.github/workflows/ingress.yml`). See the relevant workflow for the complete CI environment and additional checks; local unit tests do not replace integration or end-to-end coverage.

## Documentation checks

From `web/docs/`, run `bun run check:content` for content checks and `bun run check:diagrams` for Mermaid rendering checks. The diagram check launches Playwright Chromium. After installing the workspace dependencies, install its browser with `bun x playwright install chromium` if it is not already available. The implementation is in [check-content.ts](https://github.com/AdamOusmer/ItsBagelBot/blob/main/web/docs/scripts/check-content.ts). Run the docs build as well when changing navigation or rendering; these checks cover different failures.

## Verify and troubleshoot

A zero exit status means the selected command passed. Read skipped-test output and test setup before treating an integration path as covered. For a connection failure, check the required backend and environment described by that test or the service's `context.md`. If the diagram check cannot launch Chromium, install the Playwright browser as described above. Expand from focused tests to the relevant workflow checks before submitting a cross-service change.

See [developer setup](/guides/getting-started/) for dependencies and [RPC contracts](/reference/rpc-contracts/) for contract boundaries.
