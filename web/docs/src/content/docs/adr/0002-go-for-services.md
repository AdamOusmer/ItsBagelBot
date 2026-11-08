---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0002 - Go for services"
description: "Architecture decision: Go for services"
docType: decision
---

**Date:** 2026-01-04

**Status:** Accepted; refined by [ADR 0005](/adr/0005-elixir-twitch-ingress/).

## Context

The rewrite needed a service language suited to concurrent work and a modest runtime footprint.

## Decision

We chose Go as the primary service language for its concurrency model, runtime footprint and deployment characteristics.

## Consequences

The project must maintain Go alongside other language ecosystems. Twitch ingress later adopted Elixir for supervision ([ADR 0005](/adr/0005-elixir-twitch-ingress/)).

## References

[Go module](https://github.com/AdamOusmer/ItsBagelBot/blob/main/go.mod), [Twitch ingress](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/ingress).
