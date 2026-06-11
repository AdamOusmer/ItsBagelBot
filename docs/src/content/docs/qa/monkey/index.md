---
title: Monkey tests
description: Randomized and property-style runs against running services. Used to find crashes, deadlocks, and unexpected state.
---

Each entry below is a dated run report. Open the report for the verdict block, method, findings, and artifact
links. See [QA reports](/qa/) for the reading and authoring conventions that apply to both streams.

## Reports

| # | Date | Scope | Build | Environment | Verdict |
|---|------|-------|-------|-------------|---------|

*No runs published yet. The first report lands here after the inaugural Twitch Ingress monkey run.*

## What we run

- **Lifecycle chaos.** Kill, pause, and slow individual processes inside a service. Assert the supervisor recovers
  within its declared budget.
- **Input fuzzing.** Replay a recorded Twitch EventSub stream with bit-flipped, truncated, or out-of-order frames
  against a staging tenant.
- **Load bursts.** Short, high-rate event bursts to surface back-pressure and queue overflow handling.

## What we do *not* run

- **Anything that touches a real broadcaster.** Monkey runs use staging tenants with synthetic OAuth grants. A
  monkey run that hits a live broadcaster's chat is a bug in the harness.
- **Long soak tests.** Soak runs live in the CI nightly job, not here. This subgroup is reserved for bounded,
  reproducible runs that produce a single artifact set.
