---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Projector
description: "Maintains shared Valkey read models from owner events and serves status, live, stream information and dashboard reads."
docType: reference
---

## Responsibilities

It folds user, command, module, automation and loyalty events. Writes precede cache invalidation; invalid payloads are acknowledged while storage failures return errors for redelivery. Cold reads can fetch from owners, and background hydration repairs missing sections. Revision checks protect newer state from older updates.

## Interfaces

A durable consumer validates events, folds them into shared state and only then broadcasts invalidations. A missing dashboard section can be fetched from its owner; asynchronous hydration repairs the cache without blocking every read. Live lifecycle writes happen before acknowledging the event, while go-live followup work is best effort. Projector does not write Sesame’s `live:<id>` key.

## State and dependencies

Projector stores derived views in Valkey; data services remain authoritative.

## Failure behavior

Invalid events are acknowledged; storage errors are returned for redelivery. Revision checks protect newer updates.

## Code map

Source: [app/projector/](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/projector). See `main.go`, `hydration/`, `rpc/`, and `internal/projection/`.

## Validation

Run `go test ./app/projector/... ./internal/projection/...` from the repository root. See `hydration/` and `rpc/live.go` for ordering and read behavior.
