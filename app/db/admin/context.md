# Admin data service

Owns staff membership, staff audit history, aggregate analytics rollups and annual
analytics snapshots in `bagel_admin`. The [admin console](../../../web/admin/context.md)
provides the interface. Users owns broadcaster accounts and customer mutations;
Transactions owns billing-provider operations and giveaways.

## Terms and contracts

| Term | Meaning here |
| --- | --- |
| Staff | Persisted moderator, admin or owner membership, independent of customer access. |
| Active staff | Membership currently allowed to act; caller-supplied roles never grant authority. |
| Audit entry | An operator action with actor, target, result and timestamp. |
| Audit operation ID | Optional delivery identity that makes a committed customer mutation safe to audit repeatedly. Legacy console entries have no operation ID. |
| Analytics cell | One catalog metric, period, window and allowed dimension tuple. Distinct counts retain their original window. |
| Annual snapshot | Immutable archive of the previous calendar year's cells whose periods closed by January 1 UTC, including persisted year-end levels when available. |

Staff authentication and audit keep the existing
`bagel.rpc.admin.user.auth.*` and `bagel.rpc.admin.user.audit.*` contracts.
`bagel.rpc.internal.admin.staff.active` supplies authoritative active staff to
other services. `bagel.rpc.internal.admin.audit.append` accepts durable test-account
audit deliveries from Users, including retries after the original staff membership
changes. Public audit append checks current active membership. Analytics reads use `bagel.rpc.admin.analytics` and require an
active admin or owner. Consumers use the shared `internal/adminauth` client for
staff authorization and audit writes.

## Code ownership

| Path | Responsibility |
| --- | --- |
| [main.go](main.go) | Schema initialization, staff bootstrap, RPC registration and periodic analytics work. |
| [rpc/adminauth.go](rpc/adminauth.go), [staff_gate.go](rpc/staff_gate.go) | Identity refresh and persisted staff authorization. |
| [rpc/staff.go](rpc/staff.go), [staff_tx.go](rpc/staff_tx.go), [staff_seed.go](rpc/staff_seed.go), [staff_store.go](rpc/staff_store.go) | Staff changes, role safeguards, transactional locking and bootstrap persistence. |
| [rpc/audit.go](rpc/audit.go), [audit_append.go](rpc/audit_append.go) | Audit search, pagination, append authorization and idempotent trusted delivery. |
| [rpc/analytics.go](rpc/analytics.go), [admin_analytics.go](rpc/admin_analytics.go) | Analytics authorization, catalog validation and query replies. |
| [repository/analytics.go](repository/analytics.go) | Validated aggregate persistence and period queries. |
| [repository/analytics_snapshot.go](repository/analytics_snapshot.go) | Insert-once annual archives and historical catch-up. |
| [rollup/rollup.go](rollup/rollup.go) | Closed-period transfer from shared analytics storage, replica coordination and completion markers. |
| [ent/schema](ent/schema) | The four Admin-owned tables. Generated bindings follow these schemas. |

The worker runs on startup and periodically. January 1 capture waits for the
final flush and successful period transfers. Snapshots preserve original cells
rather than adding distinct counts or running levels across periods. An ISO
week crossing January 1 remains outside that year's snapshot because it was
still open at the cutoff. Catch-up archives years with persisted source cells;
it never invents missing history or writes empty years on a fresh deployment.
