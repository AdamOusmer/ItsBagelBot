---
title: Users
description: Manages user accounts, OAuth tokens, and status tiers.
---

The Users service (`app/users/`) is the central authority for identity in ItsBagelBot.

## Architecture

- **Ownership**: Owns the Users MySQL schema containing Twitch IDs, OAuth tokens, and account statuses.
- **Status Tiers**: Determines whether an account is `free`, `paid`, or `vip`, which dictates their access to premium features or routing lanes.
- **Token Vault**: Manages the sensitive Twitch OAuth tokens. It acts as an internal vault; other services (like Outgress) query the Users service securely over NATS RPC (`bagel.rpc.internal.tokens.*`) to obtain temporary access to perform actions on behalf of the user.

## Inter-Service Auth

NATS authorization restricts access to the `bagel.rpc.internal.tokens.*` subjects. Only authorized services (Outgress and the Users service itself) can access these subjects, ensuring that plaintext tokens are heavily guarded within the cluster mesh.
