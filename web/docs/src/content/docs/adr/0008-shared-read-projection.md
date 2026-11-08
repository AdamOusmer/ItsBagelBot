---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0008 - Shared read projection"
description: "Architecture decision: Shared read projection"
docType: decision
---

**Date:** 2026-06-09

**Status:** Accepted.

## Context

Shared read paths need data from several owners without querying each owning schema on every read.

## Decision

We chose a Valkey-backed projection maintained by the projector. Owners remain authoritative; events and owner snapshots populate the projection.

## Consequences

The projection can be stale or unavailable and needs hydration and reconciliation. Consumers must not treat it as the durable source of truth.

## References

[Projector](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/projector), [Projection](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/projection).
