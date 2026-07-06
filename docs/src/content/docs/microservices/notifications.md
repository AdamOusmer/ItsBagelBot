---
title: Notifications
description: Manages dashboard notifications and admin announcements.
---

The Notifications service (`app/notifications/`) manages persistent alerts, system announcements, and dashboard bells for broadcasters and operators.

## Architecture

- **Schema**: Backed by a MySQL database using `ent` for the schema runtime and migrations.
- **RPC Service**: Communicates exclusively over NATS Request-Reply.
  - **User RPC** (`bagel.rpc.notifications.*`): Serves the console dashboard, tracking read state (Peek vs Full Read) and fetching unread counts.
  - **Admin RPC** (`bagel.rpc.admin.notifications.*`): Allows operators to blast system-wide announcements or target direct notifications by user ID (or username via a cross-service lookup to `bagel.rpc.admin.user.get`).

## Data Lifecycle and TTL

Notifications are ephemeral to prevent unbounded database growth. The service implements tiered Time-To-Live (TTL) sweeps:

- **Default TTL** (`NOTIF_DEFAULT_TTL`, 90 days): A global hard limit.
- **Full Read TTL** (`NOTIF_FULL_READ_TTL`, 1 day): Once a user fully reads a notification, it is scheduled for quick deletion.
- **Peek TTL** (`NOTIF_PEEK_TTL`, 7 days): Opening the bell dropdown (peeking) marks notifications as seen, triggering a medium-term deletion.

### Janitor Cron

The TTL sweeps are not evaluated on the fly. The service subscribes to an internal maintenance verb (`bagel.rpc.internal.notifications.cleanup`). A Kubernetes CronJob fires this RPC periodically to sweep expired records from the database in batch.
