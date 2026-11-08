---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0006 - Data service ownership"
description: "Architecture decision: Data service ownership"
docType: decision
---

**Date:** 2026-06-09

**Status:** Accepted.

## Context

Data belonging to different domains needs explicit ownership. Users, commands, modules and transactions began as separate boundaries; the code now has additional data services.

## Decision

Data services own their schemas and expose changes through contracts instead of allowing other services to join or write their tables.

## Consequences

Cross-service workflows and deletion are eventually consistent rather than one database transaction.

## References

[Data services](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db), [Contracts](https://github.com/AdamOusmer/ItsBagelBot/tree/main/internal/domain).
