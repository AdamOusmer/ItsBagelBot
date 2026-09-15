---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0011 - Transactions Owns Giveaway Winner Records"
description: "Architecture decision record: giveaway winner history belongs to Transactions, alongside billing protection and transactional email."
---

**Date:** 2026-09-15

## Status

Accepted and implemented. Nonrecurring promotional grants have an independent fulfillment switch. Subscriber intervals and provider mutations remain gated until Tebex billing behavior is verified.

## Context

A giveaway winner is owed Premium even when Tebex renewal protection or email delivery is pending. Recording only a user's current Premium status cannot preserve that obligation or explain whether the prize was delivered. Users owns account identity and access, while Transactions already handles Tebex and sends transactional email through Resend.

## Decision

Transactions owns the authoritative giveaway winner records and their fulfillment history. Winner emails are sent through Transactions' Resend integration. Users remains authoritative for user identity and effective Premium access.

The giveaway design keeps campaign and draw records with winners in Transactions so a committed draw has one authoritative ledger. Transactions requests access changes through idempotent Users contracts using the award identity; it does not write the Users database directly. This follows the service boundaries in [ADR 0007](/adr/0007-adoption-of-per-schema-data-microservices/).

## Consequences

Winner selection, unresolved billing protection, and email progress can be traced from one Transactions award record. Email failure and billing delay do not erase the winner. Fulfillment must recover across service failures because recording the winner and committing Premium access are separate database operations.

## Alternatives considered

- **Users owns winner history:** convenient beside access grants, but splits the prize obligation from the Transactions billing and email workflow and conflicts with the requested ownership.
- **Both services own writable winner records:** creates competing histories and unclear recovery ownership. Users instead keeps access records linked to the Transactions award identity.
