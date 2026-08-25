---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0011 - Separate YouTube Ingress Service"
description: "Architecture decision record: why YouTube live chat gets its own Elixir service instead of extending the Twitch ingress"
---

**Date:** 2026-08-22

## Status

Accepted

Extends [ADR 0006](/adr/0006-adoption-of-elixir-for-twitch-ingress/) to a second edge service.

## Context

[Issue 619](https://github.com/AdamOusmer/ItsBagelBot/issues/619) adds YouTube
Live support. The ingestion problem looks superficially identical to the one
the Twitch ingress already solves — hold long-lived platform connections open,
normalize events, publish them onto NATS — so the first question was whether
YouTube belongs inside `app/ingress` (a second supervision subtree in the same
release and BEAM cluster) or in its own service.

## Decision

Build a **separate standalone Elixir service**, `app/yt-ingress`, mirroring the
Twitch ingress's structure but sharing no release, no BEAM cluster, and no
process with it.

## Reasons

1. **The connection model is inverted.** Twitch owns discovery and placement:
   the EventSub Conduit routes every subscribed broadcaster's events into a
   fixed set of shards we merely keep connected. On YouTube we own discovery:
   nothing tells us a channel is live until we poll for it. Shard ids,
   distribution strategy, scaler policy, and capacity math all diverge; merging
   means namespacing every Horde key anyway while coupling their lifecycles.

2. **Failure isolation across very different fragility profiles.** The streamList
   gRPC surface is official but young, and its quota semantics are per-project;
   the discovery loop burns quota continuously by design. A restart storm or
   quota lockout on the YouTube side must not touch Twitch shard sessions, and
   vice versa.

3. **Independent scaling and deploy cadence.** Twitch scales by conduit shard
   count against firehose throughput; YouTube scales by watched-channel count
   against a quota budget where every replica multiplies discovery cost. Their
   steady-state replica counts and rollout rhythms should not be one knob.

4. **House pattern.** Every service here is independently deployable with its own
   release, Containerfile, manifest, and CI workflow ([ADR 0001]). A second OTP
   app inside one release would make it the only multi-tenant deployable in the
   fleet and would couple the two platforms' blast radii permanently.

The cost is deliberate duplication of the stable infra modules (NATS planes,
broadcaster cache, cluster resolver, drain, health surface). That duplication is
bounded, env-driven, and matches how the Go services each own their own wiring.

## Alternatives considered

- **Second supervision subtree inside `app/ingress`** (one Elixir release): shares
  the infra modules for free, but couples deploy cadence, blast radius, BEAM
  cluster topology, and Horde key namespaces between platforms whose connection
  models share nothing. Rejected per reasons 1–3 above.
- **Go service** (`app/yt-ingress` in Go): consistent with ADR 0002's primary
  language, but it would re-create exactly the problem ADR 0006 exists because
  of — hand-rolled goroutine supervision over long-lived streams, no CRDT
  registry, no cluster-wide process ownership. The streamList surface is a
  long-lived streaming RPC; that is BEAM-shaped work.
- **Polling-only ingestion** (`liveChatMessages.list` on an interval): fully
  supported API, but ~5 s latency, quota-capped at roughly one channel's worth
  of polling inside the default project budget, and it reintroduces the exact
  HTTP-polling loop the issue set out to eliminate.

## Consequences

- New subject space `youtube.ingress.*`; consumers dispatch on payload `type`
  exactly as they do for Twitch subjects.
- The token lease contract (`bagel.rpc.youtube.token.get`) must be implemented
  by the users service before OAuth channels work; until then development runs
  on static API keys via env.
- Multi-platform command routing in sesame (issue #619 part 3) consumes both
  ingress event spaces through one normalized shape (`platform` field added).

[ADR 0001]: /adr/0001-rewriting-to-microservices/
