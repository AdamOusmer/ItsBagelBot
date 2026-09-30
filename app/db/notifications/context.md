# Notifications data service

Agent guide for `app/db/notifications`. See the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns dashboard in-app notifications, their per-user acknowledgments and expiry
in `bagel_notifications`. It serves request/reply only, with no JetStream event
consumers. Email delivery belongs to callers such as Transactions, not this app.
The console owns display and session authorization; RPC contracts and NATS access
boundaries define who can invoke the admin/user surfaces.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| Notification | One title/body/level record with a global lifetime. |
| Broadcast | Single notification visible to every user, with no target ID. |
| Direct | Notification addressed to one Twitch user ID. |
| Level | `info`, `success`, `warning`, `critical`; presentation severity. |
| `request_id` | Logical-send identity for retry deduplication. |
| Read receipt / `NotificationRead` | One acknowledgment row per notification/user. |
| Full read / `mark_read` | Acknowledgment with a short per-user visibility cutoff. |
| Peek / `mark_peeked` | Bell-dropdown acknowledgment with a longer cutoff for unread items. |
| Global expiry | Parent notification cutoff; janitor can physically delete the row. |
| Per-user expiry | Hides that notification for one user while retaining parent/receipt storage. |
| Retract / delete | Immediately remove the parent notification and cascade receipts. |
| Janitor / cleanup | Internal RPC sweep invoked by the one-shot cleanup mode. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Normal boot, TTL env defaults, RPC surfaces and `cleanup` process mode. |
| [cleanup.go](cleanup.go) | One-shot NATS client to the running service's janitor RPC. |
| [repository/notifications.go](repository/notifications.go) | Send dedup, admin/user lists, visibility predicates, acknowledgment and deletion. |
| [rpc/admin.go](rpc/admin.go) | Send/list/delete validation, direct username resolution and cache invalidation. |
| [rpc/user.go](rpc/user.go) | User list, unread count, full read and peek handlers. |
| [rpc/maintenance.go](rpc/maintenance.go) | Cleanup handler. |
| [rpc/view.go](rpc/view.go) | Ent-to-wire notification view conversion. |
| [rpc/wiring.go](rpc/wiring.go) | Shared wiring and explicit read/send/cleanup budgets. |
| [ent/schema/notification.go](ent/schema/notification.go) | Parent fields, send identity and receipt cascade. |
| [ent/schema/notification_read.go](ent/schema/notification_read.go) | Per-user cutoff and acknowledgment uniqueness. |

Shared wire shapes: [internal/domain/rpc/notifications](../../../internal/domain/rpc/notifications/).
Direct-recipient lookup uses [Users RPC](../../../internal/domain/rpc/users/).
Other `ent/` files are generated from the two schemas.

## Flow and contracts

- Admin default prefix `bagel.rpc.admin.notifications`: `.send`, `.list`, `.delete`.
  Direct sends resolve a numeric ID or username through Users' internal lookup.
- Create returns whether a row was new; replay of a request ID returns the
  existing row and avoids another insert/cache invalidation.
- User prefix `bagel.rpc.notifications`: `.list`, `.mark_read`, `.mark_peeked`.
  Lists combine broadcast and direct-to-user items, excluding global and
  per-user expiry, then return receipt state plus unread count.
- Opening the bell creates receipts for previously unacknowledged candidates.
  A later full read updates the existing receipt's cutoff; peek preserves it.
- Internal janitor defaults to `bagel.rpc.internal.notifications.cleanup`.
  The cleanup binary mode requests this RPC; it does not open MySQL itself.
- Send/retract publishes the applicable cache invalidation via RPC-plane NATS;
  this service does not publish notification data events to JetStream.

## Invariants and pitfalls

- Parent expiry and per-user expiry do different jobs. Marking read must not
  globally delete a broadcast; per-user expiry alone does not reclaim storage.
- Defaults in `main.go`: `NOTIF_DEFAULT_TTL=90 days`,
  `NOTIF_FULL_READ_TTL=24 hours`, `NOTIF_PEEK_TTL=7 days` (Go duration syntax).
  Schema-level nil expiry means no expiry; admin send normally supplies a default.
- A read receipt is unique per parent/user. Concurrent peeks can collide; preserve
  bulk-insert fallback that accepts rows another request already inserted.
- Admin paging is 20 items with at most 25 pages; user reads cap at 50.
  Unread count is calculated over the returned user list, not every stored row.
- Requests identify Twitch users; Ent notification IDs are separate integer row
  IDs and are transmitted as strings in acknowledgment requests.
- Keep handler budgets distinct: reads/acknowledgments 3 seconds, send 5 seconds,
  janitor 30 seconds. Cleanup process has a separate 60-second total budget.
- Keep RPC-only boot and `ent/runtime` initialization; health probes the SQL
  pool/RPC connection without an event-lane check.

## Focused checks

From repository root:

```sh
go test ./app/db/notifications/repository ./app/db/notifications/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/notifications/ent/schema
```

`repository/notifications_test.go` covers send dedup, visibility and expiry;
`rpc/budgets_test.go` protects handler deadlines. The operational
`go run ./app/db/notifications cleanup` requires the running service/NATS credentials
and actually deletes globally expired notifications; it is not a local test.
CI regeneration/race checks: [main.yml](../../../.github/workflows/main.yml).
