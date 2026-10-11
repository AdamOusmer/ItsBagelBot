---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Notifications
description: Manages dashboard notifications and admin announcements.
---

The Notifications service (`app/db/notifications/`) manages persistent alerts, system announcements, and dashboard bells for broadcasters and operators.

## Architecture

- **Schema**: Backed by a MySQL database using `ent` for the schema runtime and migrations.
- **RPC Service**: Communicates exclusively over NATS Request-Reply. It consumes no JetStream events and publishes none.
  - **User RPC** (`bagel.rpc.notifications.*`): Serves the dashboard with `list`, `mark_read` and `mark_peeked`, tracking read state (Peek vs Full Read) and unread counts.
  - **Admin RPC** (`bagel.rpc.admin.notifications.*`): `send`, `list` and `delete`. Operators can blast system-wide announcements or target direct notifications by user ID or username (resolved through a Users lookup). A repeated `request_id` does not create a second notification.

## Data Lifecycle and TTL

Notifications are ephemeral to prevent unbounded database growth. The service implements tiered Time-To-Live (TTL) sweeps:

- **Default TTL** (`NOTIF_DEFAULT_TTL`, 90 days): A global hard limit.
- **Full Read TTL** (`NOTIF_FULL_READ_TTL`, 1 day): Once a user fully reads a notification, it stops being shown to that user after a day.
- **Peek TTL** (`NOTIF_PEEK_TTL`, 7 days): Opening the bell dropdown (peeking) marks notifications as seen, hiding them from that user after a longer delay.

These per-user cutoffs hide a notification for one user. The row itself is deleted only when its global expiry passes.

### Janitor Cron

The TTL sweeps are not evaluated on the fly. The service subscribes to an internal maintenance verb (`bagel.rpc.internal.notifications.cleanup`). A Kubernetes CronJob (`notifications-cleanup`, daily at 03:17) runs the service binary in `cleanup` mode, which calls this RPC to sweep globally expired records. The cleanup process does not open MySQL itself.
