---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Transactions
description: Handles Tebex webhooks and checkout, and runs giveaway draws and awards.
---

The Transactions service (`app/db/transactions/`) handles the billing and giveaway workflow. Tebex is the merchant of record, so this service keeps an audit of verified webhooks, not a payment ledger.

## Architecture

- **Webhooks**: Tebex posts to `/tebex` or `/webhooks/tebex` over HTTPS (`webhooks.itsbagelbot.com` routes there). The service verifies the signature, records the event in its audit table, and applies accepted billing events synchronously through the [Users](/microservices/users/) service (`bagel.rpc.internal.billing.apply`). If Users cannot apply an event, the webhook fails so Tebex retries.
- **Checkout**: `bagel.rpc.transactions.basket_create` creates a Tebex basket. It refuses to create one when the account already has future coverage or a pending award.
- **Giveaways**: Admins preview and freeze a candidate pool from the Users eligibility snapshot, then draw winners. A draw stores the selected awards in the same transaction as the work that delivers them. A worker then prepares and commits the Premium grant in Users, protects recurring Tebex billing when needed, and sends award emails.
- **Ownership**: It owns its own MySQL schema and never opens the Users database. In-app notices go through [Notifications](/microservices/notifications/), and emails through the Resend mail adapter.

A promotional Premium award is not a Tebex subscription: Users keeps the two apart.
