---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0009 - Transactions owns giveaway awards"
description: "Architecture decision: Transactions owns giveaway awards"
docType: decision
---

**Date:** 2026-09-15

**Status:** Accepted and implemented; fulfillment paths have separate gates.

## Context

A prize obligation must survive delays in access changes, billing protection or email.

## Decision

Transactions owns giveaway draws, winner records and fulfillment history. Users remains authoritative for identity and effective access.

## Consequences

The services coordinate through idempotent contracts. Recovery must account for operations committed in different databases.

## References

[Transactions](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/transactions/giveaway), [Users](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/db/users).
