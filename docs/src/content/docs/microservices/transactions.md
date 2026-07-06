---
title: Transactions
description: Processes and records Tebex purchases.
---

The Transactions service (`app/transactions/`) handles the financial and purchase records aspect of the bot. 

## Architecture

It consumes the `data.transactions.recorded` events from the message bus. Its primary responsibility is to persist the historical ledger of Tebex purchases. It manages its own isolated MySQL database schema and does not expose this data directly to the hot path of the bot, rather it acts as an immutable ledger and validation layer for premium tier upgrades.
