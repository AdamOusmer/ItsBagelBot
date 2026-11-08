---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Validation evidence
description: Reference for recording and interpreting validation results.
docType: reference
---

Tests check behavior, reviews explain findings, and benchmarks describe workloads. Keep those claims distinct. A design or rollout note is not proof that its plan ran.

## Record validation evidence

Record the source commit, date, scope, command or method, environment and outcome. Link retained evidence and identify real dependencies, fakes and untested behavior. Keep a candidate finding separate from its validated conclusion and remediation. Record later runs as new results rather than silently replacing earlier evidence. Do not publish secret values or private payloads.

## Guides and sources

- [Run randomized checks](/qa/monkey/) to make generated failures reproducible.
- [Review a candidate finding](/qa/ai-review/) to document a trigger and its evidence.
- [Validate Twitch delivery](/qa/outgress-hot-path-canary/) to check the Outgress path.
- [Testing](/guides/testing/) provides commands; [performance work](/infrastructure/performance/) covers measurement.

Templates live in `web/docs/.qa/`. Focused repository material also lives under `docs/reviews/`, `docs/specs/`, `docs/runbooks/` and `docs/operations/`; check each document's scope and date. A passing local test does not certify a production canary.
