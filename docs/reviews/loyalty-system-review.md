# Loyalty points and gambling review

Reviewed September 25, 2026 against the current working tree, including existing uncommitted changes. Three GPT-6 Sol subagents reviewed earning, gambling, and storage independently. This is a review and proposed backlog; application code and deployment settings were not changed.

This is the historical review of the original feature-branch checkout. Current `main` already has transactional, idempotent counter batches; the PR preserves that newer implementation and adds account lifecycle fencing. The findings below describe the earlier baseline and are a backlog for the broader points/gamble system, not a claim that all defects remain on current `main`.

The original main problem was that spendable points inherit the delivery guarantees of loss-tolerant counters. Subscriptions, cheers and watchtime pass through two in-memory aggregation windows, while wagers synchronously debit and credit SQL in separate operations. Valkey currently supplies caches, cooldowns and timer claims. It should own the live economy, with atomic operations and recoverable history.

P1 means a correctness issue to fix before expanding the economy. P2 means a reliability or usability issue to address next. Product additions are listed separately from confirmed defects.

## Recommended direction: Valkey owns live points

Use persisted Valkey for balances, watch seconds, fractional earning remainders, operation results, wager cooldowns and current leaderboards. Use an audit Stream for asynchronous MySQL history and reporting. Keep NATS as the existing event transport; adding a Stream does not require replacing ingress.

| Responsibility | Proposed implementation |
| --- | --- |
| Account identity | Immutable broadcaster and viewer Twitch IDs; logins are display/lookup metadata. |
| Earn/add/set/remove/transfer | A bounded Lua script or Valkey Function validates the request, handles duplicate operation IDs, applies the mutation, records the result and appends its audit entry without interleaving. |
| Gamble | Generate the cryptographic roll before mutation. One operation resolves `all`/`half` from the authoritative balance, validates limits and cooldown, settles the net debit or credit, and records stake, roll, odds, result and resulting balance. A replay returns the stored result. |
| Watch accrual | A persisted due-time sorted set and window records, with bounded work batches and a stable ID for each channel/window/viewer award. Expiry notifications may wake workers, but cannot be the only record of unpaid work. |
| Leaderboards | Sorted sets updated by the same operation, separating current spendable points, lifetime earned points and watchtime. Keep scores within the chosen exact-integer range. |
| History and reporting | Stream consumer groups project records to MySQL with unique operation IDs; commit before `XACK`, reclaim abandoned pending work, and trim only behind durable projection/recovery watermarks. |
| Reads | Primary-consistent reads for spending decisions, duplicate results and recovery. Node-local replicas may serve explicitly stale reporting views. |

Valkey supports atomic conditional multi-key scripting, and Streams provide pending-entry acknowledgement and recovery mechanisms. These are suitable building blocks, rather than an automatic end-to-end delivery guarantee. [Valkey scripting](https://valkey.io/topics/eval-intro/), [Stream acknowledgements](https://valkey.io/commands/xack/), [pending-entry recovery](https://valkey.io/commands/xautoclaim/).

Validate key types, ranges and all expected failure cases before the first write: script atomicity prevents interleaving but does not provide SQL-style rollback after an arbitrary script error. Keep scripts and watch batches bounded to avoid blocking the entire Valkey primary. Retain operation results through the actual retry/replay horizon; do not use a short cooldown TTL as the idempotency contract.

## Prioritized fixes

### 1. P1 — Make point awards survive write failures and process crashes

**Evidence:** [earning claims dedup before handoff](../../app/twitch/sesame/modules/loyalty.go), [reporter drains its maps](../../app/twitch/sesame/engine/loyalty_reporter.go), [publish failures are dropped](../../app/twitch/sesame/engine/loyalty_reporter.go), [consumer returns success after RAM accumulation](../../app/db/loyalty/main.go), [SQL flush drains pending work](../../app/db/loyalty/repository/loyalty.go), [failed SQL chunks are discarded](../../app/db/loyalty/repository/loyalty.go).

**Failure:** A subscription or cheer can be accepted, then disappear when a worker crashes, publication fails, or the loyalty service crashes after acknowledging the event. Normal flush windows are five seconds in Sesame and fifteen seconds in the loyalty service; failures can discard complete batches. A dedup claim can suppress the replay of an award that never reached storage.

**Work:** Apply each award atomically in Valkey with its stable source identity and audit entry, then acknowledge the input according to the durability policy. Preserve batching for throughput, with individually identifiable awards and retryable partial batches. Remove RAM-only acceptance and dropped point windows from the currency path. Update the pipeline's [log-and-skip handler policy](../../app/twitch/sesame/engine/pipeline.go) so retryable currency failures enter a durable retry path without repeating completed sibling effects.

**Acceptance:** Restart after acceptance, interrupt publication, and inject storage failures. Every input award is eventually applied once, or remains visibly pending with its original ID.

### 2. P1 — Make retries return the original result instead of moving points again

**Evidence:** [award DTO has no source/batch ID](../../internal/domain/event/data/loyalty_events.go), [received rewards are added unconditionally](../../app/db/loyalty/repository/loyalty.go), [mutation requests have no operation ID](../../internal/domain/rpc/loyalty/loyalty.go), [grant claim released on RPC errors](../../app/twitch/sesame/modules/loyalty.go), [transfer claim released on RPC errors](../../app/twitch/sesame/modules/loyalty.go).

**Failure:** A lost consumer acknowledgement can double an earned batch. A transfer or grant can commit, time out, release its claim, and apply again on replay. Gamble has no event-level deduplication; the same chat message delivered after its cooldown can reroll and move points again.

**Work:** Carry one stable, namespaced operation ID from the source through the live mutation and history projection. Record the mutation result in the same Valkey operation as the balance change. Duplicates return the existing result before checking a new cooldown or choosing a new outcome. Bind the ID to request parameters so conflicting reuse is rejected. Deduplicate the MySQL projection transactionally as well.

**Acceptance:** Replay the identical event concurrently, after a restart, after cooldown expiry and after a lost response. Observe one balance movement, one wager outcome and one history entry.

### 3. P1 — Settle a gamble as one operation

**Evidence:** [stake debited before rolling](../../app/twitch/sesame/modules/gamble.go), [roll failure returns after debit](../../app/twitch/sesame/modules/gamble.go), [winning payout is a separate request](../../app/twitch/sesame/modules/gamble.go), [post-debit read can fail](../../app/db/loyalty/repository/queries.go), [RPC then discards the successful spend outcome](../../app/db/loyalty/rpc/rpc.go).

**Failure:** Start at 100, wager 50, successfully debit to 50, then fail the winning payout request. The viewer loses their stake despite winning. A crash between the calls also leaves no recoverable wager. Even a successful debit followed by a failed response read makes the caller abort settlement.

**Work:** Generate a cryptographic roll before any balance change. Submit a single idempotent Valkey wager operation that checks stake coverage and settles the final balance, records the frozen settings/outcome and appends history. On an ambiguous response, retry/query the same operation ID. An immediate refund attempt alone does not resolve uncertain commits.

**Acceptance:** Interrupt the worker before execution and after execution but before the reply. The wager is either unapplied or fully settled, and replay reports the same roll and balance.

### 4. P1 prerequisite — Give authoritative Valkey state appropriate durability

**Evidence:** [deployment explicitly uses ephemeral `emptyDir`](../../deploy/cache/valkey.yaml:732), [AOF uses every-second fsync](../../deploy/cache/config/tuning.conf:10), [replica lag gate is asynchronous](../../deploy/cache/config/tuning.conf:61), [TTL keys are eviction candidates](../../deploy/cache/config/valkey.conf:42), [read-only commands normally use a node-local replica](../../pkg/valkey/routing.go).

**Failure if promoted unchanged:** Recreating all pods loses the dataset and AOF files. Under memory pressure, TTL-based retry identities can disappear early. Local replica reads can miss a recent earning or wager result.

**Work:** Provision persistent storage and tested backups/restoration for the ledger. Prefer a dedicated persistent, non-evicting Valkey deployment for currency, isolated from disposable cache data. Establish persistence/replication acknowledgement and failover guarantees; use `pkg_valkey.Primary` for authoritative reads. Bound Stream and operation-result retention by safe recovery watermarks, with capacity monitoring and explicit backpressure.

`appendfsync everysec` allows a recent loss window, and `WAIT` alone does not make Sentinel strongly consistent. `WAITAOF` can confirm persistence of preceding writes on the same connection; connection ownership and timeout handling matter. Define and test the actual guarantee before reporting an operation as durable. [Valkey persistence](https://valkey.io/topics/persistence/), [replication limits](https://valkey.io/topics/replication/), [WAITAOF](https://valkey.io/commands/waitaof/).

**Acceptance:** Test primary failure immediately after an acknowledged mutation, replica promotion, complete pod recreation, restore/replay, memory pressure and lost acknowledgement. No success response may promise a stronger guarantee than the tested configuration provides.

### 5. P1 — Use Twitch IDs throughout point mutations

**Evidence:** [gamble reads by authenticated viewer ID](../../app/twitch/sesame/modules/gamble.go), [escrow uses login](../../app/twitch/sesame/modules/gamble.go), [spending picks a stored row by login/freshness](../../app/db/loyalty/repository/queries.go), [transfer recipient uses the same lookup pattern](../../app/db/loyalty/repository/transfer.go).

**Failure:** Viewer A leaves a stored row with login `x`, then renames. Viewer B acquires `x` and has their own positive balance recorded under a previous login. Before B's new name reaches SQL, B's gamble passes the ID-based balance check but can debit and credit A's stale `x` row.

**Work:** Address all live balances by broadcaster/viewer ID, including gamble payouts, refunds and transfers. Resolve manually typed usernames to current Twitch IDs; refresh metadata separately. Allow deliberate grants to valid users without requiring a prior award row.

**Acceptance:** Rename/recycle a login while delaying identity updates. Only the authenticated viewer's account can fund their wager, and typed-target grants/transfers resolve to the intended current account.

### 6. P2 — Enforce balance and arithmetic bounds at every mutation boundary

**Evidence:** [bet limits have no upper numeric cap](../../app/twitch/sesame/engine/gamble.go), [unchecked doubled payout](../../app/twitch/sesame/modules/gamble.go), [adjustments accept negative final balances](../../app/db/loyalty/repository/queries.go), [balance schema has no nonnegative/range constraint](../../app/db/loyalty/ent/schema/balance.go).

**Failure:** A verified standalone reproduction accepts a stake of `5,000,000,000,000,000,000`; its doubled payout wraps to `-8,446,744,073,709,551,616`. Default bet limits avoid this, but custom valid `int64` settings can reach it. At ordinary values, removing 100 from a balance of 10 stores -90.

**Work:** Choose a nonnegative exact-integer currency range, enforce it in Valkey operations, API/config validation and MySQL projection, and check all multiplication/addition before writing. Respect Lua/JSON/sorted-set numeric precision. Explicitly make over-removal floor at zero or refuse it. Validate the same effective odds and bounds shown by the dashboard; its current 1–99 help disagrees with the backend's accepted 100%.

**Acceptance:** Test minimum/maximum balances, large bets/rates, overflow, negative sets, over-removal and concurrent credits near the ceiling. Reject invalid operations without changing points or audit state.

### 7. P2 — Resolve affordability and results from authoritative state

**Evidence:** [one-minute cached balance](../../app/twitch/sesame/engine/loyalty_valkey.go), [gamble refuses before attempting a fresh spend](../../app/twitch/sesame/modules/gamble.go), [adjustment reply uses stale pre-update arithmetic](../../app/db/loyalty/repository/queries.go).

**Failure:** Cache zero, receive a sub/cheer award, then try a covered wager: it can be refused until the cache expires. `all` and `half` also use an old amount. Separately, read 100, receive a concurrent +50, grant +20: the database holds 170 but the reply can report 120.

**Work:** Resolve bet amount, affordability and returned post-operation balance inside the authoritative Valkey operation. Make ordinary point reads primary-consistent or explicitly versioned. Keep dashboard reporting caches separate from mutation decisions.

**Acceptance:** Earn points and immediately gamble with a previously cached zero. Concurrent awards cannot make the operation report a balance computed from an obsolete read.

### 8. P2 — Preserve fractional bits earnings

**Evidence:** [each cheer is rounded independently](../../app/twitch/sesame/modules/loyalty.go), [zero awards are discarded](../../app/twitch/sesame/modules/loyalty.go).

**Failure:** At the default 50 points per 100 bits, one 100-bit cheer earns 50 points; one hundred separate 1-bit cheers earn zero.

**Work:** Keep an integer fractional remainder per broadcaster/viewer in Valkey. Apply remainder and whole-point changes with the source operation ID. Specify how a rate change treats an existing remainder.

**Acceptance:** Splitting an identical total of bits into smaller cheers produces the same total reward under unchanged settings. Duplicate events do not advance the remainder twice.

### 9. P2 — Recover watch windows that miss their expiry notification

**Evidence:** [reconciler only rearms missing timers](../../app/twitch/sesame/engine/loyalty_tick.go), [arming installs a fresh five-to-six-minute timer](../../app/twitch/sesame/engine/loyalty_tick.go), [production uses a thirty-second claim](../../app/twitch/sesame/engine/loyalty_tick.go), [each snapshot pays a fixed 300 seconds](../../app/twitch/sesame/engine/loyalty_tick.go).

**Failure:** If all listeners miss a timer expiry, recovery starts a new countdown and never pays the completed window. Failures/restarts reduce accumulated time even for continuously present chatters. A lease is not a durable record that a specific window was paid.

**Work:** Persist due/completed windows in Valkey, claim/recover overdue jobs, and make each viewer's window award idempotent. Bound work with a cursor/batches. Define snapshot eligibility and catch-up limits; do not infer continuous presence during an outage or promise exact video watchtime. Make stale stream status and missing chatters permissions observable.

**Acceptance:** Miss expiry across restart, run two workers, resume after a lease expires, and interrupt a partially paid window. Every eligible recorded window is paid once, without inventing unobserved presence.

### 10. P2 — Give set/reset an ordering contract

**Evidence:** [three independent loyalty replicas](../../deploy/k8s/loyalty.yaml:29), [absolute SQL set](../../app/db/loyalty/repository/queries.go), [delayed additive award flush](../../app/db/loyalty/repository/loyalty.go).

**Failure:** Replica A buffers +100. A moderator sets the balance to zero through replica B and sees success. A flushes later and the old award restores 100. Flushing only the replica handling the command cannot order all outstanding work.

**Work:** Route all live mutations through the same Valkey account ordering. Define whether set applies to operations already accepted or events occurring before a cutoff. If a reset is intended to discard earlier delayed earnings, carry a durable epoch/cutoff; serialized execution alone cannot decide source-event ordering.

**Acceptance:** Delay a pre-reset award on another worker, set/reset the account and deliver that award afterward. The result matches the documented policy across retries and recovery.

### 11. P2 — Separate viewer query cooldowns from management operations

**Evidence:** [all `!points` verbs share a five-second cooldown](../../app/twitch/sesame/modules/loyalty.go), [cooldown key contains channel and command only](../../app/twitch/sesame/engine/dispatch.go).

**Failure:** Alice's `!points` suppresses Bob's point query and a moderator's add/set/remove command during the same window. A viewer can keep triggering the shared throttle, making the economy feel unresponsive.

**Work:** Use per-viewer read throttles and separate mutation throttles. Keep gamble's per-viewer limit inside the wager operation, and return actual remaining cooldown time and useful failure feedback.

**Acceptance:** Two viewers can query balances independently; point queries cannot silently block moderator writes.

### 12. P2 — Expose failures instead of presenting disabled or empty state

**Evidence:** [configuration lookup/decode failures become quiet disabled state](../../app/twitch/sesame/engine/loyalty_config.go), [watch processing then stops quietly](../../app/twitch/sesame/engine/loyalty_tick.go), [dashboard silently converts failed standings reads to an empty leaderboard](<../../web/dashboard/src/routes/(app)/loyalty/+page.server.ts:42>), [point-query errors return no reply](../../app/twitch/sesame/modules/loyalty.go).

**Work:** Distinguish disabled, unavailable and malformed config. Show last successful watch award, chatters permission/mod status, pending history age and storage health. Report retry/backpressure failures at actionable severity. Preserve available settings when standings fail, but mark standings unavailable. Give mutation/query failures a clear status; resolve ambiguous operations by ID before advising retries.

**Acceptance:** Remove chatters permissions, break a config read and stop the history projector. The dashboard identifies the failing component and affected work rather than claiming no viewers earned points.

## Product work to approach StreamElements polish

These are feature/semantics gaps, not evidence about StreamElements' internal reliability.

1. **Subscriber earning controls.** Add a configurable subscriber watch multiplier and an exclusion list for other bots/accounts. The current watch loop excludes only this bot and gives every other chatter the same rate. Keep watchtime tracking independently configurable from currency earning. StreamElements documents subscriber multipliers and earning exclusions in its [loyalty overview](https://support.streamelements.com/hc/en-us/articles/10474478470290-Loyalty-System-Overview).
2. **A complete management view.** Search/paginate viewers, show current points, lifetime earned points, watchtime and a reasoned transaction history. Add grant/set/remove, watchtime correction, import/export and deliberate account-transfer tools with actor attribution. The current page has rates and a read-only top ten. StreamElements documents [leaderboard management](https://support.streamelements.com/hc/en-us/articles/10474478802578-Loyalty-Leaderboard-Overview) and [point/watchtime adjustments](https://support.streamelements.com/hc/en-us/articles/12428772165394-Manually-Move-Someone-Else-s-Points-and-Watch-Time-Data).
3. **Familiar chat commands.** Add `!watchtime [user]`, `!points [user]`, rank and watchtime/lifetime leaderboards. Add percentage wagers and `k`/`m` shorthand. Say explicitly when `all`/`half` is capped by the maximum wager. StreamElements documents [loyalty commands](https://docs.streamelements.com/chatbot/commands/default/) and [roulette arguments](https://docs.streamelements.com/chatbot/modules/roulette).
4. **Subscription semantics.** Label the current resub source as a shared resub message: it does not observe every automatic renewal. Decide whether to support renewal reconciliation, tier-specific gifter rewards and duration-based awards. Current recipient sub/resub rewards use tier multipliers; gifter rewards use gift count without tier. This is a policy decision. Twitch's [EventSub reference](https://dev.twitch.tv/docs/eventsub/eventsub-reference/) defines the shared-message and gift payloads; gift events do not supply subscription duration.
5. **Modern Bits coverage.** The bot consumes `channel.cheer`, with no `channel.bits.use` handler. Add loyalty support for `power_up` and `custom_power_up` if Bits spending should count. Choose one canonical source for ordinary cheers or filter overlap so two topics cannot double-award them. Twitch documents these types in the [Channel Bits Use event](https://dev.twitch.tv/docs/eventsub/eventsub-reference/#channel-bits-use-event).
6. **Clear economy controls.** Replace hidden zero/default and negative/off conventions with visible enabled switches and effective rates; make tier multipliers, win chance, payout and limits explicit. Show expected earning per hour and the effect of odds on point supply. Current StreamElements intervals vary by channel rather than universally using ten minutes, so cadence alone is not a quality benchmark. [StreamElements changelog, July 25, 2024](https://docs.streamelements.com/changelog).

## Implementation order

1. Build the persisted Valkey ledger infrastructure, immutable identity contract, bounded numeric rules, atomic operation/result format and history Stream. Add failure/replay tests before switching writers.
2. Move earn, gamble, transfer and moderator mutation paths to those operations. MySQL becomes an idempotent projection; remove SQL balance mutation from the chat hot path.
3. Replace watch expiry-only correctness with persisted due windows, then add fractional Bits earning, fresh point reads, independent cooldowns and visible health.
4. Add subscriber/exclusion controls, paid-event coverage, history/management tools and command compatibility.

Migrate current SQL balances under a controlled writer cutover with a recorded watermark, drain or explicitly reconcile the existing RAM/broker windows, and validate balances/leaderboards before enabling the new writers. Do not let both old additive SQL writers and new Valkey writers independently own the same currency.

## Verification and limits

Existing focused tests passed for engine/module loyalty and gamble behavior, transfers, and the loyalty repository/RPC. The gambling review reproduced the large-stake overflow in a standalone temporary Go program without changing repository code. The repository already has a test that explicitly permits dropping failed flush chunks, so a green existing suite does not establish durable currency delivery.

Commands used by the reviewers included:

```text
GOCACHE=/tmp/bagel-loyalty-review-cache go test ./app/db/loyalty/repository ./app/db/loyalty/rpc
GOCACHE=/private/tmp/bagel-loyalty-review-gocache go test ./app/twitch/sesame/modules ./app/twitch/sesame/engine ./app/db/loyalty/repository -run 'Gamble|BalanceTransfer|Transfer|BoundedAmount' -count=1
GOCACHE=/private/tmp/bagel-loyalty-review-go-cache go test ./app/twitch/sesame/engine ./app/twitch/sesame/modules -run 'Test(Loyalty|WatchTick|RearmAfterFailure|Secfix.*Dedup)' -count=1
VALKEY_TEST_ADDR=<isolated-localhost-instance> GOCACHE=/private/tmp/bagel-loyalty-review-go-cache go test ./app/twitch/sesame/engine -run '^Test(WatchTick|Loyalty|RearmAfterFailure)' -count=1 -v
```

The initial engine loyalty run skipped real-Valkey lifecycle/reconfirmation tests because `VALKEY_TEST_ADDR` was unset. A subsequent run against an isolated loopback Valkey server passed, including `TestWatchTickSettleSuccess` and `TestWatchTickFireLifecycle`, with no skips in the selected run. The temporary server was stopped and its data removed afterward. Live cluster behavior, Sentinel failover, production balances and scope grants were not inspected. Findings concern the checked-in working tree, not a claim that every failure has occurred in production.

## Current-main follow-up

The PR port preserves current main's idempotent counter-batch transactions and trial handling. Its watchtime account fences also protect modules cleanup. The broader recreation audit remains open: commands/fetches and other unchanged deletion consumers still clean up by Twitch ID, so this PR does not establish account-incarnation isolation for every service. Source-user deletion publication also retains the existing SQL/event delivery guarantees. Treat transactional source events and service-wide cleanup fencing as separate backlog work.
