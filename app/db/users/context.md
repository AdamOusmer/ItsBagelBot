# Users data service

Agent guide for `app/db/users`. Read the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md), especially account activity and Premium.

## Responsibility and boundaries

Owns registered broadcaster identity, bot-enable state, preferences, Twitch token
custody, contact email, effective access, promotional grants and dashboard delegation in the MySQL schema selected by `DB_SCHEMA`. It provisions the shared `DATA`
stream and `DATA_DLQ`; Sesame owns Twitch ingress streams. Transactions owns provider HTTP calls
and giveaway campaigns/draws, while Users owns authoritative eligibility/access.
Admin owns staff membership, audit history and aggregate analytics in its own schema.
Modules, Commands and Loyalty own their separate feature records. Users also
owns all desktop device-authorization state and token logic (device grants,
authorization codes, Ed25519 access-token signing); the dashboard owns HTTP
input/CSRF/rate-limiting and forwards to Users over RPC, and the controls
gateway only verifies access tokens and listens for revocation.

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
| `leaderboard_page_hidden` | Inverted visibility flag for the public leaderboard; false preserves public-page default. |
| `songqueue_page_hidden` | Inverted visibility flag for the public song queue page; false preserves public-page default. |
| Account instance / `AccountCreatedAt` | Account creation Unix **microseconds**, distinguishes delete/recreate incarnations. |
| State revision | Monotonic mutation version emitted with user projection/change events. |
| Token / grant | Twitch OAuth token custody; in giveaway files, grant means a Premium interval. |
| Contact email | Real address sealed in `email_enc`; legacy `email` is a synthetic placeholder. |
| Premium grant | Users-owned promotional interval identified by giveaway+award. |
| Staff | Admin-owned active role membership: moderator, admin or owner; distinct from Premium/VIP. |
| Delegation | Single-use invitation leading to scoped access to an owner's dashboard. |
| Preference write | Locale/cursor/onboarding/creator-code batching; active intent changes persist immediately through lifecycle CAS. |
| Payment failed | Separate billing condition; does not itself mean access has expired. |
| Device grant | One desktop app authorization: `device_id` (stable across refresh) and `grant_id` identify it; owns the current refresh-token family. |
| Authorization code | Single-use, 60s-lived PKCE code; stored only as a SHA-256 hash. Reuse revokes whatever grant it produced. |
| `controls.revoked.<device_id>` | Core-NATS (non-JetStream) fire-and-forget revocation broadcast; the controls gateway is its only intended consumer. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Mandatory keyset, schema/stream boot, consumers, RPC registration, account analytics reconciliation and expiry sweeps. |
| [repository/users.go](repository/users.go) | Registration, cached safe views, lookup, token custody, account deletion and change/reprojection events. |
| [repository/preferences.go](repository/preferences.go) | Per-user/field coalescing, transactional flush, failure isolation and pending preference overlay state. |
| [repository/contact_email.go](repository/contact_email.go) | Sealed contact-email writes and internal read. |
| [repository/billing.go](repository/billing.go) | Verified billing event ordering/ownership and paid-access expiry. |
| [repository/giveaways.go](repository/giveaways.go) | Full eligible pool, exclusion counts, test-account control, prepare/commit/cancel/expire grants and coverage. |
| [repository/grant_access.go](repository/grant_access.go) | Reconcile/project effective access from committed grant coverage. |
| [repository/users_admin.go](repository/users_admin.go) | Admin directory/statistics, searches and token-reset helpers. |
| [repository/delegations.go](repository/delegations.go) | Invite issuance, one-time consumption and scoped dashboard grants. |
| [rpc/dashboard.go](rpc/dashboard.go), [admin.go](rpc/admin.go) | User-facing setup/settings/state and admin account operations. |
| [../../../internal/adminauth/](../../../internal/adminauth/) | Trusted Admin RPC client for role checks, current staff eligibility and audit writes. |
| [rpc/billing.go](rpc/billing.go), [giveaways.go](rpc/giveaways.go) | Internal access application/eligibility/grant RPCs and invalidations. |
| [rpc/tokens.go](rpc/tokens.go), [email.go](rpc/email.go), [projection.go](rpc/projection.go), [counts.go](rpc/counts.go), [delegation.go](rpc/delegation.go) | Scoped read/custody/delegation endpoints. |
| [rpc/bot_token.go](rpc/bot_token.go) | Bot OAuth grant validation for the dedicated bot-token save path. |
| [rpc/budgets.go](rpc/budgets.go) | Explicit deadlines protected by tests. |
| [rpc/deviceauth.go](rpc/deviceauth.go) | Desktop device-authorization RPCs (`rpc.dashboard.device.*`); OAuth-error mapping for the token endpoint. |
| [repository/deviceauth_codes.go](repository/deviceauth_codes.go), [deviceauth_refresh.go](repository/deviceauth_refresh.go), [deviceauth_grants.go](repository/deviceauth_grants.go), [deviceauth_cleanup.go](repository/deviceauth_cleanup.go) | Code issuance/exchange, refresh rotation with reuse detection, grant listing/revocation, and the expired-code/stale-grant sweep. |
| [tokensign/tokensign.go](tokensign/tokensign.go) | Ed25519 access-token signing/verification (`v1.<payload>.<sig>`), keyed by `kid`. |
| [ent/schema/](ent/schema/) | User, Tokens, PremiumGrant, Delegation, LifecycleErasure, DeviceGrant, AuthorizationCode and pending AdminAuditDelivery definitions. |

Shared contracts: [Users RPC](../../../internal/domain/rpc/users/),
[billing RPC](../../../internal/domain/rpc/billing/),
[projection RPC](../../../internal/domain/rpc/projection/),
[data events](../../../internal/domain/event/data/data_events.go),
[cache invalidations](../../../internal/domain/invalidate/).
Edit schemas rather than generated Ent code.

## Flow and contracts

- Dashboard defaults to `rpc.dashboard.*`; admin accounts to
  `rpc.admin.user.*`. Admin owns the `.auth` and `.audit` surfaces; Users
  asks Admin to authorize every staff account operation.
  Desktop device authorization sits at `rpc.dashboard.device.*`
  (`create_code`, `exchange`, `refresh`, `list`, `revoke`, `revoke_all`) under
  the same wildcard export.
- Registration persists identity immediately; contact-email capture is a separate
  best-effort step. Sensitive token/email reads use internal RPCs, not projections.
- Locale/cursor/onboarded/creator-code preferences coalesce
  per user/field over 2 seconds / 256 items. Each user gets one merged UPDATE per
  flush window. Dashboard/admin active changes use SetActiveNow and persist
  immediately, as do status, banned state, tokens and commands-page visibility.
- Committed mutations publish full `data.users.changed`; broadcasts invalidate
  each instance's local safe-view cache. Grouped reprojection requests replay
  persisted account state for downstream rebuilding.
- Deletion publishes `data.users.deleted` with account instance and deletion time after deleting the
  owning account/token rows; feature services perform their own eventual cleanup.
- Deletion first writes `erasures/<id>-<accountCreatedAt>` to the R2 erasure
  ledger (`ERASURE_R2_*`) and fails if that write fails, then revokes Twitch
  tokens. `ReplayErasures` (boot + every 15m) re-deletes any account a database
  restore brought back and resends `data.users.deleted` for records under 1h
  whose account is gone; the ledger lives outside MySQL so it survives losing it.
- Transactions calls `rpc.internal.billing.apply` and Users' internal
  giveaway prefix for eligibility, coverage and grant lifecycle. `main.go` holds
  subject overrides, token/email/count reads and the projection subject.

Lifecycle authority: `repository/lifecycle.go` owns immediate intent, independent bot moderation/grant facts, admission generations, incident suppression, bounded recovery and indexed due replay. `lifecycle_projection_pending` persists publication failures without changing read revision on acknowledgment. Legacy seeds run 200 per due pass and unknown access stays degraded. `LifecycleErasure` retains incarnation-fenced cleanup after deletion; completed markers expire after seven days.

Device authorization: `CreateDeviceAuthorizationCode`/`ExchangeDeviceCode` redeem a
single-use code atomically (conditional `UPDATE ... WHERE consumed_at IS NULL`,
same optimistic-concurrency shape as `delegations.go`); redeeming an
already-consumed code revokes whatever grant it produced, win or lose the
race. `RefreshDeviceGrant` rotates the refresh token on every use and keeps
one prior hash; presenting that prior hash inside a 10s grace window is a
quiet `invalid_grant` (covers a concurrent retry), presenting it after the
grace window revokes the whole grant. `RevokeAllDeviceGrants` is the single
path shared by the dashboard's `revoke_all` RPC, `SetBanned(true)` and
`Delete`/erasure; a revoke commits in MySQL first and then best-effort
publishes core NATS `controls.revoked.<device_id>` per device — a failed
publish is logged, never un-revokes. Only `ExchangeDeviceCode`/`RefreshDeviceGrant`
need the Ed25519 signer; a missing/invalid `DEVICE_AUTH_SIGNING_KEY` fails
just those two closed and leaves the rest of the RPC surface (and the rest of
Users) running.

## Invariants and pitfalls

- Do not equate `status="paid"` with a recurring Tebex subscription. VIP is
  permanent access; promotional intervals preserve independent billing identity.
- Grant coverage is committed and half-open (`start <= now < end`). Award identity
  `(giveaway_id, award_id)` is globally unique across Users, preventing delivery to
  a second account on retry. Intervals use UTC microsecond precision.
- Eligibility comes from the complete Users snapshot, not a paged admin list:
  banned/inactive/not-onboarded/test/VIP/current-staff exclusions, with pending
  active/onboarding preference values overlaid. Current staff membership comes from
  Admin over trusted internal RPC; unavailable membership fails the pool read.
  Former staff can be eligible. Test-account flag changes commit atomically with an
  audit-delivery intent; startup and periodic delivery retry the same operation ID
  until Admin confirms it, even if the original staff member has since been revoked.
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
- Staff hierarchy is an Admin authorization boundary; Users must recheck the
  persisted active role through Admin and never trust a client-supplied role. Delegation invites are single use.
- Subscription sweeps default to 5 minutes with 24-hour Tebex grace; Premium-grant
  sweeps run independently at 30 seconds. Each replica runs its own periodic sweeps.

## Focused checks

From repository root:

```sh
go test ./app/db/users/repository ./app/db/users/rpc ./app/db/users/tokensign
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/users/ent/schema
```

Choose preference tests for batching, giveaways tests for eligibility/access,
watchtime_lifecycle tests for instance metadata, and contact-email/admin tests
for custody/roles; bot-token tests protect its dedicated identity/scopes gate.
Dashboard commands-page integration tests cover visibility and
invalidation. `deviceauth_test.go` covers the device-authorization business
rules (replay/grace/revocation/erasure) on sqlite; `deviceauth_mysql_test.go`
proves no double-spend under real concurrent exchange/refresh against MySQL
(needs `MYSQL_TEST_DSN`, otherwise skipped); `tokensign` has a fixed-vector
test pinned against `scratchpad/token-test-vector.json`. CI regeneration/race
checks: [main.yml](../../../.github/workflows/main.yml).
