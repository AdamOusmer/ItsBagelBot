# Inactive channel lifecycle implementation

Local branch: `feature/inactive-channel-lifecycle`.
Baseline: `985b8f150c0898be716c3e77f1753caf6f9665ae`, including Twitch race fix PR #1246.
Status: implementation complete; final Astra reviews clear and checks passed. [PR #1278](https://github.com/AdamOusmer/ItsBagelBot/pull/1278) is open. No production deployment or cleanup was performed.

## Behavior

- A confirmed bot ban or timeout closes ordinary runtime admission. Normal jobs, timer execution, watchtime, optional moderator checks, token warming and settings hydration respect the same lifecycle state.
- Matching bot unban events start bounded recovery. Explicit owner Disable remains authoritative. Broadcaster ban/unban control subscriptions are kept separately from ordinary bot work; listener loss is shown honestly in the dashboard.
- Dormancy starts after 30 days without useful activity, subject to bounded live-state reconciliation. A useful command, enabled feature event or admitted go-live wakes a healthy dormant account. Passive reads and unknown commands do not keep an account active.
- One durable incident suppresses repeated warnings. Notice creation has a transactional rolling 24-hour cap, stable delivery identity, deadline and expiry. Recovery retracts its notice even if recovery races delivery.

## Existing ownership and shared code

Users owns a compact current lifecycle value on its existing row and indexed due columns. It uses the existing state revision and creation-time incarnation for conditional writes. There is no new scheduler service or append-only lifecycle event table.

`internal/domain/lifecycle` defines shared states and admission rules; `internal/channelruntime` provides the existing projection/Users fallback and engagement adapter. Outgress runs bounded due work through the existing internal RPC and worker machinery. Notifications retains ownership of content, receipts, cooldown guards and cleanup.

Recovery permits at most three attempts for each operation. Engagement writes are coalesced authoritatively to 15 minutes, except an immediate dormant wake. Missing live evidence can defer dormancy for at most 24 hours; stale-positive evidence permits one probe per window. Neither mechanism becomes a perpetual blocked-account poll.

Control-listener repair also persists its due time and permits at most three attempts. Exhaustion leaves listener health visibly unavailable until fresh authorization or explicit repair. Normal subscription removal has compact durable progress, with incomplete cleanup backing off to once per day; unban cancels obsolete cleanup before it can remove replacement subscriptions.

## Storage

Registry hashes expire after seven days, with a sorted hot index and bounded pruning. Legacy set conversion is cursor based. Listing hot channels is paginated; no scheduler scans all historical account IDs.

Notification cleanup removes expired parents in batches of 500 and retains incident delivery receipts through their fixed replay deadline. Current incident suppression remains in Users after notification expiry. Read receipts carry account incarnation; an exact-index migration replaces only the obsolete uniqueness rule and preserves unrelated indexes.

The existing owner/admin account-delete action captures a minimal, durable erasure record with the original deletion time and exact subscription ownership. Cleanup retries separately for Notifications and Outgress. It preserves shared broadcast content and replacement-account data. Completed erasure records are retained seven days, then pruned in bounded batches. Dormancy, bans and owner Disable do not delete accounts or settings.

Production storage size, expired-row backlog, query plans at production cardinality and legacy permanent-notice classification were not measured locally. A blanket expiry backfill is intentionally excluded: legacy translated warning titles are not a reliable category identifier. Inspect those rows with production read access before any category-specific backfill.

## Review

Implementation was split among GPT-6.1 Sol agents. Independent GPT-6 Astra reviews covered Users/admission and live-state ordering, Notifications/dashboard/storage, and Twitch/Ingress/recovery.

Review-driven fixes include account-recreation races, timeout preservation, stale poll/SSE ordering, erasure reply contracts, legacy receipt migration, listener permission degradation, obsolete subscription cleanup, engagement racing dormancy and go-live retry baseline capture. Regression tests accompany the fixes.

The CodeScene follow-up separates lifecycle transitions, due selection, subscription effects and notification transaction steps into named domain helpers. Shared activity and recipient values keep related identity fields together. It retains all state fences and transaction boundaries without changing CodeScene rules. Astra reviewed the refactor against the initial implementation commit.

## Validation

- Final local CodeScene delta against `origin/main` reports zero introduced or degraded findings. Existing rule configuration was unchanged.
- Combined Go race suite passed with disposable real Valkey at `127.0.0.1:16387`: lifecycle/live/RPC domains, channelruntime, projection, watchtime, Users, Notifications, Projector, Twitch Outgress, Sesame and NATS configuration tests. This includes the original PR #1246 regressions and new lifecycle ordering cases. The NATS tests run disposable brokers and generated test identities.
- All 281 Ingress ExUnit tests passed.
- All 24 focused dashboard/kit Bun tests passed; Svelte checks reported zero errors and zero warnings.
- Translation catalogs pass for all six configured locales. Existing overview keys retain their paths; new lifecycle strings are grouped under `overview.lifecycle`.
- Container builds now include `internal/channelruntime` for Projector, Outgress and Sesame. Tests use the shared `pkg/codec` JSON API, including the repository-wide import guard.
- Notification migration tests begin with the old schema and prove receipt uniqueness upgrades without dropping unrelated indexes. SQL repository tests use SQLite; deployed MySQL upgrade and representative-cardinality query plans remain staging checks.
- `git diff --check` passed. The disposable Valkey instance was stopped after validation.

## Release notes

1. Apply the additive Users and Notifications schema changes and the targeted receipt-index migration before depending on new fields. Keep the shared contracts and lifecycle-aware consumers in the same coordinated release.
2. Generate/sign the normal broker account artifacts from the approved `deploy/messaging/accounts.yaml` changes through the existing release process. Both shared and node-local RPC subjects are covered. This task did not access signing secrets or modify a running broker.
3. New OAuth requests include `channel:moderate` for ban/unban events and `moderation:read` for an explicit targeted ban check. Existing grants may require renewed consent. Missing control permissions must not block otherwise valid normal enrollment; the dashboard indicates when automatic unban recovery is unavailable.
4. Run a staging flow against the deployed SQL/Valkey/NATS stack: active → bot ban → repeated go-live → unban mid-stream; owner Disable during repair; deletion and re-registration. Confirm the one-warning cap and absence of blocked optional API calls.

PR #1246's two- and ten-minute offline confirmations, five-minute demand window and version-aware live writes remain separate from lifecycle admission. Lifecycle generations additionally reject work queued before a ban or owner Disable.
