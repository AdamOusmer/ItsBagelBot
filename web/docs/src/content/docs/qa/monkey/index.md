---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Run randomized checks
description: Make failures from randomized checks reproducible.
docType: how-to
---

Use randomized inputs to exercise a chosen invariant and retain enough information to reproduce a failure.

## Prerequisites

Choose the code, fixture and invariant to test. Start with the repository's existing [tests and benchmarks](/guides/testing/). Use fakes for platform effects or an isolated test account.

## Procedure

1. Record the source commit, seed, fixture, limits and exact command for the run.
2. Exercise relevant malformed events, duplicates, cancellation, reconnects and bounded queue pressure.
3. Assert an invariant, such as no duplicate committed award or no action after expiry, rather than only checking that the process stayed alive.
4. If a failure occurs, retain the original evidence, reduce the scenario to a repeatable case and add a focused regression test.

## Verify and record

Reproduce the failure with the recorded inputs and run the regression test. A future report can use `NNNN-YYYY-MM-DD-scope.md`, with the seed and reproduction command near the top. See [validation evidence](/qa/) for the other fields to retain.

## Limitations

This guide does not designate a randomized harness or claim that a run occurred. No numbered randomized-run reports are published in this section.
