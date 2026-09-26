# Watchtime stability, request correctness and tenant isolation

Reviewed September 25, 2026. Three GPT-6 Sol subagents reviewed scheduling, Twitch requests and tenant/storage boundaries. This extends the [loyalty review](../../docs/reviews/loyalty-system-review.md). The findings below record the implementation before fixes; their line references refer to that snapshot.

## Implementation status

The subsequent parallel implementation replaces expiry-only attendance with durable Valkey due windows, saved pages, shared retries and versioned leases. Atomic account/session admission feeds a replayable Stream; SQL transactionally deduplicates operations and viewer/window credits. Canonical account creation timestamps fence deletion/recreation and loyalty configuration ownership. Requests use bounded workers, absolute deadlines, correlated replies, explicit pagination and eligibility checks before every HTTP attempt. Invalid cursors start a fresh window, successful scans preserve cadence, and incremental discovery avoids tenant starvation.

The existing shared quota authority in this checkout is NATS JetStream. The implementation charges that manager rather than creating a competing Valkey token budget. Valkey owns attendance state, admission and provider reset cooldowns. Balances remain in SQL. The PR preserves current main's newer transactional, idempotent counter batches for subscription/bits earnings and adds deletion fencing; gamble settlement remains a separate path. The original findings below concern the earlier feature-branch snapshot.

See the [rollout procedure](../../docs/operations/watchtime-rollout.md) for retained-storage migration, account reprojection and explicit canonical resaving of unstamped loyalty configurations. These deployment prerequisites have not been applied to a cluster.

Normal tests pass for all touched packages. Targeted real-Valkey recovery/isolation tests and Go race checks pass; mocked HTTP and a local NATS broker exercise request deadlines, queueing, token identity and retries. SQL posting tests use SQLite; production MySQL and live Twitch behavior have not been exercised.

A broader opt-in engine run also exposed shared-key interference in three existing emote-play tests. Each failing case passed when run independently after clearing its disposable fixture keys. Those tests/code were not changed; the watchtime fixtures use separate tenant IDs and pass together against real Valkey.

## Original findings

The broadcaster/viewer namespaces and bot-token routing are correct in the paths inspected. No direct cross-tenant browser access or response mix-up was found. Watchtime still has concrete lifecycle, fairness and recovery defects: paused accounts can retain running clocks, deleted balances can be recreated by pending rewards, chatter requests bypass the shared rate budget, and one channel's slow listing can block other channels.

## What is already correct

- Schedule and claim keys include broadcaster ID. Reporter aggregation uses `(broadcaster ID, viewer ID)`, and SQL queries/uniqueness preserve that pair. The same viewer in two channels has separate balances and watchtime.
- The requested broadcaster is passed to Twitch and to subsequent awards. Duplicate viewers within a listing are removed, invalid/zero IDs are rejected, and the bot is excluded. See [accrue](../../app/twitch/sesame/engine/loyalty_tick.go) and [reporter identity](../../app/twitch/sesame/engine/loyalty_reporter.go).
- `GetChatters` uses the configured bot as `moderator_id`, the bot's token, the target channel as `broadcaster_id`, and a page size of 1,000. This matches Twitch's documented user-token authorization branch. The empty broadcaster argument to `ExecuteAs(IdentityBot)` does not select another tenant's token. [Request construction](../../app/twitch/outgress/internal/twitch/chatters.go), [Twitch Get Chatters](https://dev.twitch.tv/docs/api/reference/#get-chatters).
- Dashboard actions derive tenant identity from the authenticated server session, with delegation checked before access. Posted loyalty forms do not select arbitrary `user_id`. See [effectiveId](../../web/dashboard/src/lib/server/board.ts:24) and [module access](../../web/dashboard/src/lib/server/module-page.ts:44).
- NATS request inboxes correlate replies with their callers. Missing/disabled/unreadable loyalty module configuration prevents accrual; stream state and module settings are checked again after chatter fetching. These checks help, but are not an atomic lifecycle boundary.

## Fix list, in priority order

### 1. P1 — Fence awards when an account is paused, removed or changes eligibility

**Evidence:** [Arm](../../app/twitch/sesame/engine/loyalty_tick.go), [fire](../../app/twitch/sesame/engine/loyalty_tick.go), [accrue](../../app/twitch/sesame/engine/loyalty_tick.go), [account pause semantics](../../app/db/users/repository/users.go), [deletion](../../app/db/loyalty/repository/queries.go), [later INSERT/upsert](../../app/db/loyalty/repository/loyalty.go).

The independent watch clock checks enabled loyalty and live state, but never authoritative registration/active/ban/trial status. A previously enabled live channel can keep receiving watch awards after account pause, provided the bot still has Twitch chatter access. The reconciler can rearm its surviving live key. Fresh trial or unregistered channels without enabled module configuration fail closed; this is a stale lifecycle-state defect, not an unconditional trial bypass.

Deleting an account's SQL loyalty rows also leaves pending earnings untouched. A later flush or delayed earned message can recreate the deleted balances/watchtime. Clearing one replica's map would not fence other replicas or already queued messages.

**Fix:** Maintain authoritative tenant admission and an account generation in Valkey. Carry the generation through schedule, request and award records. Check it atomically at award acceptance; pause/deletion/ban/trial transitions invalidate old work and cancel schedules. Keep a deletion tombstone through the delivery/replay horizon, with explicit semantics for account recreation. Reject old generations in SQL projection too.

**Acceptance:** Pause or delete during pagination; deliver buffered rewards from two replicas afterward; recreate the account. Old work must not award or resurrect deleted data. Test that changing channel A's admission does not stop channel B.

### 2. P1 — Charge every chatter page against the existing shared bot-token budget

**Evidence:** [direct ExecuteAs call](../../app/twitch/outgress/internal/twitch/chatters.go), [direct client execution](../../app/twitch/outgress/internal/twitch/client.go), [worker bot-token budget](../../app/twitch/outgress/internal/worker/buckets.go), [normal admission](../../app/twitch/outgress/internal/worker/buckets.go).

Chatter RPC bypasses the worker's Valkey rate admission. A 30-page watch listing spends 30 real Twitch requests without decrementing the corresponding internal budget. Other channels' bot-token work can receive 429s despite the internal limiter showing capacity. Twitch documents user-token buckets per client ID and user. [Twitch rate limits](https://dev.twitch.tv/docs/api/guide/#twitch-rate-limits).

**Fix:** Use shared Valkey admission before each page and applicable HTTP retry, keyed by actual token identity. Preserve broadcaster attribution for fairness. Give watch reads a bounded share so a large channel cannot consume all capacity needed for other channels and moderation.

**Acceptance:** Run paginated watch reads and ordinary bot requests from multiple outgress replicas against one quota. Every attempt must be accounted for, and small channels must make progress during a large listing.

### 3. P2 — Bound chatter concurrency and honor the caller's deadline while queued

**Evidence:** [serial subscription](../../app/twitch/outgress/rpc/chatters.go), [inline handler contract](../../pkg/bus/rpc.go), [fresh background timeout after dequeue](../../pkg/bus/rpc.go), [12-second caller timeout](../../app/twitch/sesame/engine/loyalty_tick.go).

Requests on a subscription run sequentially. With three simultaneous eight-second listings, later callers exceed their twelve-second deadline while queued. The handler still starts a fresh ten-second budget after dequeue, so abandoned requests can continue consuming Twitch capacity. Node-local routing concentrates this effect on a subscription; timeout does not trigger failover.

**Fix:** Move this read endpoint to the existing bounded [RPC pool](../../pkg/bus/rpc_pool.go), with a propagated absolute deadline and bounded admission. Discard expired jobs before HTTP, coalesce identical channel/window requests, and schedule fairly by channel. Concurrency must respect the shared token budget. Leave unrelated mutation RPCs on their existing path.

**Acceptance:** A slow large channel must not block a small channel for the entire listing. Expired queued requests must issue zero HTTP calls. Test pool saturation, shutdown and duplicate requests across replicas.

### 4. P2 — Stop reporting incomplete chatter lists as successful attendance

**Evidence:** [30-page cap and cursor reset](../../app/twitch/outgress/internal/twitch/chatters.go), [success with continuation discarded](../../app/twitch/outgress/internal/twitch/chatters.go), [reply lacks completeness metadata](../../internal/domain/rpc/manage/manage.go).

Reproduced with a mocked 40,000-chatter listing: exactly 30 pages were requested, 30,000 users were returned with `err=nil`, and the remaining cursor was discarded. Every new listing starts at the beginning. With stable ordering, the same tail can miss every tick; the comment claiming it misses one tick is inaccurate.

**Fix:** Return completeness, continuation, observation time and window identity. Store bounded, tenant-scoped pagination progress in Valkey and fairly resume within the window's deadline. Handle invalid/repeated cursors explicitly. Do not assume a saved Twitch cursor remains valid indefinitely or that a long multi-page fetch is an instantaneous attendance snapshot.

**Acceptance:** A stable 31+ page dataset must not repeatedly exclude the same tail. Verify deadlines, cursor expiry/repetition, changing attendance and one award per viewer/window across resumed pages.

### 5. P2 — Prevent old offline events from deleting newer session schedules

**Evidence:** [loyalty lifecycle handler drops event version](../../app/twitch/sesame/modules/loyalty.go), [unconditional DEL](../../app/twitch/sesame/engine/loyalty_tick.go).

Reproduced through the production loyalty handler: the versioned live store rejects an older offline event and keeps the newer stream live, but loyalty still forwards `Disarm` and erases its schedule. Reconciliation starts a fresh five-to-six-minute countdown, potentially after another minute's delay. Per-replica sequencing does not reject stale event timestamps.

**Fix:** Store live session/version with schedule state. Atomically condition Arm/Disarm and final awards on that version. A rejected old offline event must not mutate a newer session's clock.

**Acceptance:** Deliver old offline after new online on different replicas, including immediately before a due window. The newer schedule and accrued window must survive.

### 6. P1 — Persist due windows and apply each viewer/window award once

**Evidence:** [expiry-triggered work](../../app/twitch/sesame/engine/loyalty_tick.go), [reconciliation only rearms](../../app/twitch/sesame/engine/loyalty_tick.go), [award has no window ID](../../app/twitch/sesame/engine/loyalty_tick.go), [unused production identity helper](../../app/twitch/sesame/engine/loyalty_tick.go).

The expiring key is the only due-time record. A missed notification loses that window; reconciliation schedules a later window rather than recovering the missed one. A short claim prevents simultaneous work temporarily, but does not bind a durable award to a particular window. Earnings still pass through the lossy RAM aggregation path described in the broader review. Valkey documents that disconnected Pub/Sub clients lose notifications. [Valkey keyspace notifications](https://valkey.io/topics/notifications/).

**Fix:** Use persisted due-time sorted sets plus window records and fenced worker leases. Derive award identity from the stored channel/session/window/viewer, not whichever wall-clock bucket happens to process the job. Atomically check admission/session/window deduplication, update points/watchtime and append the audit Stream. Resume partial batches after failure. Expiry notifications may wake workers; due records are the recovery source.

**Acceptance:** Disconnect notifications, crash after a partial batch, expire a lease and retry an ambiguous write. Work remains recoverable, each observed viewer/window posts once, and channel identities never share dedup keys. Define lateness policy explicitly; a later snapshot cannot reconstruct unknown attendance during an outage.

### 7. P2 — Keep retry/error state and Twitch reset time in Valkey

**Evidence:** [HTTP errors discard reset metadata](../../app/twitch/outgress/internal/twitch/chatters.go), [RPC collapses failure category](../../app/twitch/outgress/rpc/chatters.go), [per-replica streak](../../app/twitch/sesame/engine/loyalty_tick.go).

Rate-limited replies become generic failures, so the scheduler cannot follow Twitch's reset time. Its failure count lives in each process: a second replica restarts quick retries even after the first reached backoff. This reset was reproduced with two clock instances. Persistent failures are also logged as possible scope/moderator trouble regardless of actual category.

**Fix:** Return typed `rate_limited`, `missing_scope`, `not_moderator`, `unavailable` and `expired` results, with `retryAt` where applicable. Store per-channel attempt count, next attempt and last error in Valkey. Apply token-wide cooldown for 429s and jitter retries. Twitch instructs callers to use `Ratelimit-Reset` after 429. [Official guidance](https://dev.twitch.tv/docs/api/guide/#twitch-rate-limits).

**Acceptance:** Alternate winners across replicas and verify one continuous retry policy. A 429 must suppress token-wide requests until the appropriate reset; one channel's lost moderator status must not suppress other channels.

### 8. P2 — Separate a requested live recheck from a successful confirmation

**Evidence:** [hour-long guard](../../app/twitch/sesame/engine/loyalty_tick.go), [asynchronous publish](../../app/twitch/sesame/engine/loyalty_tick.go).

The reconfirm guard is retained when publication is admitted, before a successful Twitch result. A later publish failure or refused worker job can leave no fresh confirmation for that guard period. Accrual precedes the recheck. Existing tests cover immediate publish rejection, not admitted publication that fails afterward.

**Fix:** Store `confirmedAt`, live session and confirmation result when the Twitch check actually succeeds. Use a short pending lease for outstanding checks and retry failed/expired work. Define the freshness requirement for awards; stale/unknown live state should be visible and retryable.

**Acceptance:** Admit but fail publication, refuse a job, time out Twitch and deliver a late old confirmation. None may masquerade as fresh confirmation for the current session.

## Valkey responsibilities and isolation contract

| State | Required behavior |
| --- | --- |
| Tenant admission | Registered/active eligibility and account generation; authoritative checks at request admission and award commit. |
| Live session | Versioned session and successful confirmation timestamp; stale events/results cannot change newer work. |
| Schedule | Persisted due times, window boundaries, bounded pagination progress, lease token, retry state and explicit lateness policy. |
| Awards | Immutable broadcaster/viewer IDs, session/window identity, atomic generation check and deduplication with balance/watchtime mutation and audit append. |
| Shared Twitch capacity | Budget by actual token/client identity; fair per-channel admission and token-wide reset cooldown. Sharing the bot token is intentional, so quota accounting must be shared too. |
| Recovery/history | Stream pending work and idempotent SQL projection; deleted generations stay fenced through replay. |

Use primary-consistent reads/scripts for admission, lease ownership and award decisions. Replica-backed views are suitable for explicitly stale displays. The original loyalty review's Valkey persistence/eviction prerequisites remain necessary before these records become authoritative. Bound script batches and validate expected failures before writes.

The internal trust boundary is service-level NATS authorization, not per-human credentials. The chatter handler currently checks only nonempty broadcaster ID and Twitch bot access; enrollment/generation validation should be added. A caller holding authorized worker credentials can already choose a broadcaster payload. No public browser route that permits this was found, so this is internal hardening and lifecycle enforcement, not a demonstrated browser exploit.

Watchtime is currently estimated chat presence: each accepted listing credits 300 seconds. Twitch reports connected chat users with delayed joins/leaves; it does not verify video playback or continuous viewing. Preserve that product meaning when choosing recovery and sampling rules. [Twitch endpoint semantics](https://dev.twitch.tv/docs/api/reference/#get-chatters).

## Validation and limits

Five temporary Go overlay tests passed without changing application files:

1. Different channel requests used their own broadcaster ID, configured bot moderator, bot bearer token and independent pagination state.
2. A 40k listing reproduced 30k returned users, a discarded continuation and successful return.
3. Production loyalty lifecycle handler disarmed after the live version check rejected an old offline event. This used a fake schedule; the actual unconditional Valkey deletion was separately inspected.
4. The same viewer in channels 7 and 9 produced separate earned DTOs with correct tenant identity, 300 seconds each, and no duplicate/bot award.
5. A second clock replica restarted quick retry after the first replica had reached longer backoff.

The earlier review also passed focused existing loyalty tests and an isolated real-Valkey lifecycle/reconfirmation run. The new tests prove the named local behaviors, not production load performance or live Twitch authorization. Rate-budget bypass, deletion resurrection, queued deadline loss and asynchronous reconfirmation were traced statically; no production service calls were made.

Suggested implementation order: admission/generation fencing and shared rate admission first; then deadline-aware fair request execution and explicit pagination; then durable windows/idempotent awards, session-version protection and shared recovery state. Acceptance should include all of these behaviors before describing watchtime as stable.
