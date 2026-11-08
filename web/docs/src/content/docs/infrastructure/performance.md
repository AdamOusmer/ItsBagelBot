---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Measure performance
description: Measure the code path, preserve its invariants, and report reproducible results.
docType: how-to
---

A throughput number needs a workload. Payload size, cache state, output rate, persistence, replication and dependency latency all change what the result means. Keep them with the result.


## Prerequisites and outcome

Choose the operation and its correctness requirements, record the baseline commit, and use the Go toolchain specified by `go.mod`. A local benchmark can establish a repeatable comparison for its fixture; testing real dependency boundaries also requires those dependencies and their configuration. Do not compare runs with different workloads as if they measured only a code change.

## 1. Run a focused baseline

The repository has engine benchmarks under `app/twitch/sesame/engine`, worker benchmarks under `app/twitch/outgress/internal/worker`, and bus benchmarks in `pkg/bus/hotpath_bench_test.go`.

```sh
# Run from the repository root.
go test -run '^$' -bench . -benchmem ./app/twitch/sesame/engine
go test -run '^$' -bench . -benchmem ./app/twitch/outgress/internal/worker
```

Save the output and confirm the command exits successfully. Read the benchmark setup first. An in-memory output sink measures engine work without proving publish latency. A fake platform server measures the local HTTP path without proving Twitch capacity.

## 2. Measure the affected boundary

For a dependency change, add the real boundary back into the workload: actual Valkey operations for limiter or projection changes, acknowledged publication for JetStream changes, and representative payloads for codec changes. `deploy/k8s/bus-bench` is a separate deployment-oriented harness; it is not required to work on the engine locally.

Record the commit, command, toolchain, fixture, warm/cold state, concurrency, duration, errors and latency distribution. Keep publish acknowledgement and end-to-end completion separate. Report final lag as well as the send rate.

## 3. Change the code and preserve behavior

- Premium and standard lanes have separate admission rules. A faster limiter must preserve which bucket is consumed on each denial.
- A retry after an ambiguous write can duplicate a side effect. Keep the shared package's retry policy.
- A cache shortcut needs an invalidation and repair path. Include the first request after eviction in the comparison.
- A stale action can be useless even if it eventually succeeds. Preserve message age and expiry checks.
- A process-local result does not establish fleet-wide coordination.

Repeat the same workload before and after a change. Investigate allocation or CPU profiles when they identify a real hot path. Keep dated benchmark artifacts as evidence; do not turn a past cluster result into a current capacity guarantee.

[Testing](/guides/testing/) covers correctness checks. [Investigate latency](/infrastructure/observability/) covers stage attribution.

## 4. Verify, compare and troubleshoot

Run the relevant correctness tests alongside the benchmark. A successful benchmark command and a lower number do not establish correctness or end-to-end capacity. If results vary, check concurrency, warm-up, cache state, competing work, errors and dependency latency; repeat with the same inputs before drawing a conclusion. Keep the command and raw output with the commit and environment so another contributor can reproduce the comparison. Use [Investigate latency](/infrastructure/observability/) to locate a slow boundary.
