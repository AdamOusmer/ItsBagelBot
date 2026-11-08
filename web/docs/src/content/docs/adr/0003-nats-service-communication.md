---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0003 - NATS for service communication"
description: "Architecture decision: NATS for service communication"
docType: decision
---

**Date:** 2026-05-23

**Status:** Accepted.

## Context

Services need request/reply, event delivery and coordination without operating a separate system for each pattern.

## Decision

We chose NATS for request/reply, JetStream event delivery and coordination.

## Consequences

NATS is a shared dependency on critical paths. JetStream delivery can repeat, so consumers must handle redelivery. Retention is finite and the bus is not the system of record.

## References

[Bus](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/bus), [Contracts](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/domain).
