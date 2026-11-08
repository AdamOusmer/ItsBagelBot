---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0001 - Rewrite as services"
description: "Architecture decision: Rewrite as services"
docType: decision
---

**Date:** 2025-12-25

**Status:** Superseded in part by [ADR 0005](/adr/0005-elixir-twitch-ingress/); the rewrite and service boundaries remain.

## Context

The first implementation was hard to extend to multiple tenants, and its Twitch connection lifecycle was difficult to supervise.

## Decision

We chose a rewrite as services in the same repository for isolation, maintainability, scaling and learning. [ADR 0005](/adr/0005-elixir-twitch-ingress/) refines the ingress language and supervision choice.

## Consequences

Distributed operations, network latency and eventual consistency become part of the system.

## References

[Services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app).
