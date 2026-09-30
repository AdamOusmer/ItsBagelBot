# Users data service

Agent guide for `app/db/users`. Read the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md), especially account activity and Premium.

## Responsibility and boundaries

Owns registered broadcaster identity, bot-enable state, preferences, Twitch token
custody, contact email, effective access, promotional grants, staff/audit records
and dashboard delegation in `bagel_users`. It provisions the shared `BAGEL_DATA`
stream and `BAGEL_DLQ`; Sesame owns Twitch ingress streams. Transactions owns provider HTTP calls
and giveaway campaigns/draws, while Users owns authoritative eligibility/access.
Modules, Commands and Loyalty own their separate feature records.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| User / account | Registered Twitch broadcaster; natural primary key is Twitch ID. |
| Username / display name | Login used for lookup/URLs / public label preserving Twitch casing. |
| `is_active` | Bot-enable setting; not recent chat activity. |
| `status` | Stored `free`, `paid`, `vip` access projection; `paid` can include giveaway coverage. |
| `subscription_source` | Access/billing origin such as Tebex, admin or giveaway; not a campaign identity. |
| Subscription reference | Provider recurring identity used to reject stale lifecycle events. |
| `commands_page_hidden` | Inverted visibility flag; false preserves public-page default. |
| Account instance / `AccountCreatedAt` | Account creation Unix **microseconds**, distinguishes delete/recreate incarnations. |
| State revision | Monotonic mutation version emitted with user projection/change events. |
| Token / grant | Twitch OAuth token custody; in giveaway files, grant means a Premium interval. |
| Contact email | Real address sealed in `email_enc`; legacy `email` is a synthetic placeholder. |
| Premium grant | Users-owned promotional interval identified by giveaway+award. |
| Staff / `AdminUser` | Active role membership: moderator, admin or owner; distinct from Premium/VIP. |
| Delegation | Single-use invitation leading to scoped access to an owner's dashboard. |
| Preference write | Background SetActive and locale/cursor/onboarding/creator-code batching; interactive active changes persist immediately. |
| Payment failed | Separate billing condition; does not itself mean access has expired. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Mandatory keyset, schema/stream boot, consumers, RPC registration, staff bootstrap and expiry sweeps. |
| [repository/users.go](repository/users.go) | Registration, cached safe views, lookup, token custody, account deletion and change/reprojection events. |
| [repository/preferences.go](repository/preferences.go) | Per-user/field coalescing, transactional flush, failure isolation and pending preference overlay state. |
| [repository/contact_email.go](repository/contact_email.go) | Sealed contact-email writes and internal read. |
| [repository/billing.go](repository/billing.go) | Verified billing event ordering/ownership and paid-access expiry. |
| [repository/giveaways.go](repository/giveaways.go) | Full eligible pool, exclusion counts, test-account control, prepare/commit/cancel/expire grants and coverage. |
| [repository/grant_access.go](repository/grant_access.go) | Reconcile/project effective access from committed grant coverage. |
| [repository/users_admin.go](repository/users_admin.go) | Admin directory/statistics, searches and token-reset helpers. |
| [repository/delegations.go](repository/delegations.go) | Invite issuance, one-time consumption and scoped dashboard grants. |
| [rpc/dashboard.go](rpc/dashboard.go), [admin.go](rpc/admin.go) | User-facing setup/settings/state and admin account operations. |
| [rpc/adminauth.go](rpc/adminauth.go) | Staff hierarchy, bootstrap membership, authorization and audit surfaces. |
| [rpc/billing.go](rpc/billing.go), [giveaways.go](rpc/giveaways.go) | Internal access application/eligibility/grant RPCs and invalidations. |
| [rpc/tokens.go](rpc/tokens.go), [email.go](rpc/email.go), [projection.go](rpc/projection.go), [counts.go](rpc/counts.go), [delegation.go](rpc/delegation.go) | Scoped read/custody/delegation endpoints. |
| [rpc/bot_token.go](rpc/bot_token.go) | Bot OAuth grant validation for the dedicated bot-token save path. |
| [rpc/budgets.go](rpc/budgets.go) | Explicit deadlines protected by tests. |
| [ent/schema/](ent/schema/) | User, Tokens, PremiumGrant, Delegation, AdminUser and AdminAudit definitions. |

Shared contracts: [Users RPC](../../../internal/domain/rpc/users/),
[billing RPC](../../../internal/domain/rpc/billing/),
[projection RPC](../../../internal/domain/rpc/projection/),
[data events](../../../internal/domain/event/data/data_events.go),
[cache invalidations](../../../internal/domain/invalidate/).
Edit schemas rather than generated Ent code.

## Flow and contracts

- Dashboard defaults to `bagel.rpc.dashboard.*`; admin accounts to
  `bagel.rpc.admin.user.*`, with `.auth` and `.audit` surfaces for staff operations.
- Registration persists identity immediately; contact-email capture is a separate
  best-effort step. Sensitive token/email reads use internal RPCs, not projections.
- Locale/cursor/onboarded/creator-code preferences and background SetActive coalesce
  per user/field over 2 seconds / 256 items. Each user gets one merged UPDATE per
  flush window. Dashboard/admin active changes use SetActiveNow and persist
  immediately, as do status, banned state, tokens and commands-page visibility.
- Committed mutations publish full `data.users.changed`; broadcasts invalidate
  each instance's local safe-view cache. Grouped reprojection requests replay
  persisted account state for downstream rebuilding.
- Deletion publishes `data.users.deleted` with account instance after deleting the
  owning account/token rows; feature services perform their own eventual cleanup.
- Transactions calls `bagel.rpc.internal.billing.apply` and Users' internal
  giveaway prefix for eligibility, coverage and grant lifecycle. `main.go` holds
  subject overrides, token/email/count reads and the projection subject.

## Invariants and pitfalls

- Do not equate `status="paid"` with a recurring Tebex subscription. VIP is
  permanent access; promotional intervals preserve independent billing identity.
- Grant coverage is committed and half-open (`start <= now < end`). Award identity
  `(giveaway_id, award_id)` is globally unique across Users, preventing delivery to
  a second account on retry. Intervals use UTC microsecond precision.
- Eligibility comes from the complete Users snapshot, not a paged admin list:
  banned/inactive/not-onboarded/test/VIP/current-staff exclusions, with pending
  active/onboarding preference values overlaid. Former staff can be eligible.
- Every user update increments state revision through the schema hook. Keep
  `ent/runtime` imported and propagate instance/revision with events/projections.
- Billing apply compares provider event time and recurring reference; old
  subscription cancellation must not revoke a newer agreement or VIP/admin access.
  Attributable declined renewals carry a separate payment-failed condition.
- The dedicated bot_token_set flow skips staff membership gating but verifies
  configured TWITCH_BOT_USER_ID against caller, target, Twitch-validated token
  owner and required bot/chat scopes. Never broaden it to arbitrary account grants.
- Tokens and email are AEAD-sealed with owner binding. Unknown OAuth expiry is
  treated as expired by downstream token readers; never assume valid forever.
- Login collisions after Twitch renames are possible: username index is not
  unique and lookup selects the most recently updated matching row.
- Staff hierarchy is an authorization boundary; bootstrap IDs and audit records
  must not be replaced by a client-supplied role. Delegation invites are single use.
- Subscription sweeps default to 5 minutes with 24-hour Tebex grace; Premium-grant
  sweeps run independently at 30 seconds. Each replica runs its own periodic sweeps.

## Focused checks

From repository root:

```sh
go test ./app/db/users/repository ./app/db/users/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/users/ent/schema
```

Choose preference tests for batching, giveaways tests for eligibility/access,
watchtime_lifecycle tests for instance metadata, and contact-email/adminauth tests
for custody/roles; bot-token tests protect its dedicated identity/scopes gate.
Dashboard commands-page integration tests cover visibility and
invalidation. CI regeneration/race checks: [main.yml](../../../.github/workflows/main.yml).
