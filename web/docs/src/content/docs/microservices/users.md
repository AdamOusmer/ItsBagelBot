---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Users
description: Manages user accounts, Twitch token custody, access status, staff and delegation.
---

The Users service (`app/db/users/`) is the central authority for identity in ItsBagelBot.

## Architecture

- **Ownership**: Owns the Users MySQL schema containing Twitch IDs, preferences, sealed OAuth tokens and contact email, account statuses, promotional Premium grants, staff roles with an audit log, and dashboard delegations.
- **Status Tiers**: Determines whether an account is `free`, `paid`, or `vip`, which dictates their access to premium features or routing lanes. `paid` can come from a Tebex subscription or from a giveaway grant; `vip` is permanent.
- **Events**: Publishes `data.users.changed` (with a state revision) and `data.users.deleted`, and broadcasts cache invalidations. Feature services clean up their own data after a deletion.
- **RPC**: Serves the dashboard (`bagel.rpc.dashboard.*`), admin account operations (`bagel.rpc.admin.user.*`), billing application (`bagel.rpc.internal.billing.apply`), giveaway eligibility and grants, delegation, and the secret-free projection (`bagel.rpc.internal.projection.users.get`).
- **Token Vault**: Manages the sensitive Twitch OAuth tokens. It acts as an internal vault; other services (like Outgress) query the Users service securely over NATS RPC (`bagel.rpc.internal.tokens.*`, verbs `get` and `save`) to obtain access to perform actions on behalf of the user. Tokens and contact emails are encrypted at rest.

## Inter-Service Auth

NATS authorization restricts access to the `bagel.rpc.internal.tokens.*` subjects. Only authorized services (Twitch Outgress and the Users service itself) can access these subjects, ensuring that plaintext tokens are heavily guarded within the cluster.
