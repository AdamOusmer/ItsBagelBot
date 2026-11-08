---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0004 - MySQL for durable state"
description: "Architecture decision: MySQL for durable state"
docType: decision
---

**Date:** 2026-05-23

**Status:** Accepted.

## Context

Durable relational state needs database capacity. The original decision favored managed capacity and reduced database operations over self-hosting.

## Decision

We chose MySQL for durable relational state, with service-owned schemas rather than a shared application schema.

## Consequences

Persistence and migrations are coupled to MySQL. Cross-service data access goes through service contracts.

## References

[Database](https://github.com/AdamOusmer/ItsBagelBot/tree/main/pkg/db), [Data services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db).
