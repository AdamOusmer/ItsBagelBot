---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Admin
description: Staff-only SvelteKit console for accounts, the shard fleet, lanes, giveaways, trials and deploys, reachable over the tailnet.
---

The Admin console (`web/admin/`) is the operators' SvelteKit (SSR) app. It replaces the earlier read-only Go admin tool. It owns no data: every read and write is a NATS RPC against the owning service, and it never opens MySQL.

Access has two layers. The network is one: the app is served through the Tailscale ingress only (`ORIGIN` is the tailnet name, `deploy/k8s/console-admin.yaml`), with no public hostname. Identity is the other: an operator signs in with Twitch and must be active staff. Each action repeats its own role check.

## Responsibilities

- **Shards**: Live state of the [Twitch Ingress](/microservices/twitch-ingress/) shard fleet (`twitch.ingress.admin.shards.get`, with scale and autoscale controls), refreshed from `twitch.ingress.status.>` events.
- **Users**: Inspect accounts, ban, grant access, set test flags, reset an account, restart its EventSub subscriptions, and open a view-as session on a broadcaster dashboard (`bagel.rpc.admin.user.*`).
- **Lanes**: Inspect and administer the JetStream pipelines and their delivery rates.
- **Giveaways**: Create campaigns, preview and freeze the pool, draw winners, and retry awards through [Transactions](/microservices/transactions/).
- **Trials**: Manage observed trial channels (`twitch.ingress.admin.trials.*`).
- **Notifications, audit, staff and secrets**: Send announcements through [Notifications](/microservices/notifications/), read the operator audit log, manage staff roles, and rotate service credentials.
- **Deploys**: Owner-only deployment runs with plan, approval and live events, backed by the deploy RPC adapters.

## Access control

Staff roles are `moderator`, `admin` and `owner`. A single role table maps each named access key to the lowest role allowed. Mutations go through audited helpers. A button hidden in the UI is a courtesy, not a guard.

## Deployment

`deploy/k8s/console-admin.yaml` runs 3 replicas with rolling updates and a PodDisruptionBudget (`minAvailable: 1`). It is exposed with an Ingress of class `tailscale`. Environment settings (Doppler secret `console-admin-env`) include the NATS and Valkey endpoints and the RPC subject prefixes it uses (`NATS_ADMIN_SUBJECT`, `NATS_ADMIN_USER_SUBJECT_PREFIX`, `NATS_ADMIN_DEPLOY_SUBJECT_PREFIX`, and others). See [Networking](/infrastructure/networking/) for the two-plane model that makes "tailnet only" meaningful.

## References

- [Twitch Ingress](/microservices/twitch-ingress/): the service whose shards this console observes.
- [Networking](/infrastructure/networking/): the tailnet posture.
- [RPC contracts](/reference/rpc-contracts/): the `bagel.rpc.admin.*` surface.
