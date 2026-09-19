---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0012 - Kit Variables Manifest Is the Public Inventory"
description: "Architecture decision record: the bot resolver owns behaviour, the kit manifest owns the public variable inventory and copy, and a Go golden fixture binds the two."
---

**Date:** 2026-09-18

## Status

Accepted. Implemented in stacked branches starting with `feat/kit-variables-manifest`.

## Context

Reply variables were described in six hand-kept places across Go, kit, dashboard and marketing, with no check between them. Lists drifted: samples disagreed, aliases were missing on one side, one variable existed only in the dashboard chips.

## Decision

- The Go scope chain (`app/twitch/sesame/engine/scope`) is the only authority on what a token expands to.
- `web/kit/lib/variables/` is the only authority on which variables are public, their forms, samples, category and copy keys. Dashboard, marketing builder and the guide derive from it.
- A golden fixture (`scope/testdata/token_catalog.golden.json`), written by the Go test behind a flag and read by a kit test, fails CI when the two inventories disagree in either direction.

## Alternatives rejected

- Generate the TypeScript manifest from Go at build time: puts a Go build in front of web CI, and copy still needs a locale home outside Go.
- Keep the marketing catalog as the source and have the dashboard import it: inverts the dependency direction (marketing already depends on kit).
- Manifest with no parity test: the status quo that produced the drift.

## Consequences

- Adding a variable is a Go change plus a kit change plus locale keys; forgetting any one fails a test.
- Surface-specific meaning of a shared name (`{user}` on a follow alert) lives on the module reply in the kit module catalog, not on the variable.
