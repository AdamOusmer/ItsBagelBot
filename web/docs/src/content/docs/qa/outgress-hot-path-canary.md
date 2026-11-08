---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Validate Twitch delivery
description: Check Outgress admission, pause state, retries and HTTP effects together.
docType: how-to
---

Outgress turns queued actions into external effects. Validate the whole decision path before claiming that a local optimization improved delivery.

## Prerequisites

Read the integration-test prerequisites and choose the code and workload to validate. Use real Valkey to verify atomic script behavior; a stub cannot prove it. Review these boundaries:

| Concern | Source |
| --- | --- |
| Worker dispatch and action policy | `app/twitch/outgress/internal/worker` |
| Channel state and pause snapshots | `app/twitch/outgress/internal/channels` |
| Ordered admission | `pkg/ratelimit/valkey.go` |
| HTTP request and response handling | `app/twitch/outgress/internal/twitch` |
| Acknowledgement, retry and expected nack | `pkg/bus` |

## Procedure

1. Check that a standard-lane denial preserves shared capacity rules and a second-bucket denial preserves the ordered limiter's documented first-bucket consumption.
2. Test pause propagation across replicas and recovery from a missed invalidation. Exercise stale state and startup before readiness as well as warm reads.
3. Cover HTTP success, rate limits, authorization failure, cancellation and bounded response draining.
4. Test expiry and redelivery. Distinguish ambiguous external effects from safely retryable work.
5. Run the focused suite from the repository root:

   ```sh
   go test -race ./app/twitch/outgress/... ./pkg/ratelimit/... ./pkg/bus/...
   ```

## Verify and record

Confirm that the relevant tests passed and record their scope and environment. For a deployment comparison, retain the commit, workload, lane mix, warm or cold state, error and retry counts, latency distribution and observations of pause propagation. See [validation evidence](/qa/) and [performance work](/infrastructure/performance/).

## Limitations and troubleshooting

A process-local snapshot does not establish fleet behavior. Reused local connections do not establish Twitch's connection protocol or latency. A NATS acknowledgement cannot prove an exactly-once platform effect. If an integration check cannot reach its dependencies, record that limitation rather than treating it as a pass. This guide does not report a completed production canary. See [Outgress](/microservices/outgress/) for ownership.
