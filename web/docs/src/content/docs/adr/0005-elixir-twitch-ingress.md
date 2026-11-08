---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0005 - Elixir for Twitch ingress"
description: "Architecture decision: Elixir for Twitch ingress"
docType: decision
---

**Date:** 2026-05-23

**Status:** Accepted; supersedes [ADR 0001](/adr/0001-rewrite-as-services/) in part.

## Context

The original Twitch connection lifecycle was difficult to supervise. [ADR 0002](/adr/0002-go-for-services/) chose Go as the primary service language.

## Decision

We chose Elixir and OTP for Twitch ingress. Supervision gives long-lived connections an explicit place to recover from failures. This refines the ingress portion of [ADR 0001](/adr/0001-rewrite-as-services/).

## Consequences

The project maintains a second runtime and toolchain. The decision to use services remains in force.

## References

[Ingress](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/ingress/lib/ingress/application.ex), [Ingress Mix](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/ingress/mix.exs).
