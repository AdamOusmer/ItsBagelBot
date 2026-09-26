# Watchtime comparison and second review

Reviewed September 25–26, 2026. Three GPT-6 Sol reviewers examined the scheduler, request path and storage/lifecycle boundaries. The original review changed documentation only. The implementation follow-up below addresses its findings. Public upstream source was read, not executed or copied into the application.

## Implementation follow-up — September 26

Sol agents implemented fixes in parallel and Luna independently crosschecked integration. Findings below describe the original reviewed state; implementation now includes source-owned user-state revisions and unknown-delete fences, cancellable token waits with final attempt admission, header-time provider reset publication, actual Twitch stream identity reconciliation, a ten-minute unfetched collection budget and seven-day replay retention protected by unpaid-outbox/SQL watermarks. Shared Helix profiles are reused from `pkg/ratelimit`; the existing primary-Valkey client, quota manager and consumer lifecycle remain authoritative.

Regression coverage includes slow 429 bodies, revocation during refresh, two-hour saved-page recovery, changed provider stream identity, page yielding across different tenant sizes, atomic retention/admission and replay rejection after SQL cleanup. Existing structured logging now carries page timing/freshness, quota denial and SQL posting/outbox age. Real-MySQL concurrency tests are available but skipped without a test server. Sustained fleet throughput and infrastructure failover/restore remain release verification work. See the [updated rollout guide](../../docs/operations/watchtime-rollout.md) for prerequisites and remaining evidence.

Luna's final crosscheck found an additional cold-hydration admission gap and a related ordering race between user and module fills. Both now use the existing account restoration operation in their shared projection writers. Real-Valkey regressions cover cold recovery, tombstones, old-incarnation replies and either section write order; hydration wiring is unchanged.

A subsequent [two-reviewer debate](../../docs/reviews/watchtime-debate-review.md) reproduced module-revision loss after settings expiry. Loyalty now retains its account-scoped revision in permanent admission metadata. The final fix also preserves refetch for missing known rows and bootstraps trusted warm revision state when old snapshots omit loyalty.

## Verdict

Our measurement model matches established loyalty systems: estimate attendance from chat presence while live and award at an interval. The implementation has useful recovery and isolation mechanisms, but **it is not yet justified to call it better than StreamElements or production hardened**. This review found a same-account status-ordering defect plus request and recovery gaps; the follow-up implemented fixes, while production verification remains outstanding.

StreamElements and Streamlabs publish behavior, not their full backend implementations or failure-rate measurements. Their documentation cannot prove that our infrastructure is stronger than theirs. The open-source comparison below provides inspectable code, but a single-channel bot and a multi-tenant service have different requirements.

## Established reference behavior

| Reference | Verified behavior | Comparison with ours |
| --- | --- | --- |
| [StreamElements loyalty documentation](https://docs.streamelements.com/loyalty) and [official overview](https://support.streamelements.com/hc/en-us/articles/10474478470290-Loyalty-System-Overview) | Live viewer-list sampling; watchtime/points accumulate on ten-minute intervals. The overview describes watchtime as an estimate. Settings include subscriber multipliers and viewer exclusions. | Same basic sampling model. Our five-minute interval gives smaller award steps and roughly doubles polling demand for equivalent listings; it does not prove actual playback or higher uptime. Subscriber watch multipliers and configurable exclusions are not present in our current watch configuration. |
| [Streamlabs Cloudbot loyalty guide](https://streamlabs.com/content-hub/post/cloudbot-101-loyalty-points) | Configurable payout interval, recommended 5–60 minutes; separate base rewards for lurkers and additional rewards for chat activity; no earning while offline. | Our five-minute live-presence payout fits this model. We currently have a fixed interval and a uniform watch payout, rather than Cloudbot's separate activity payout. |
| [PhantomBot time system, pinned source](https://github.com/PhantomBot/PhantomBot/blob/75479b56ed92b7acf33c9d3bfabec9a0ace29625/javascript-source/core/timeSystem.js#L527) and [viewer cache](https://github.com/PhantomBot/PhantomBot/blob/75479b56ed92b7acf33c9d3bfabec9a0ace29625/source/com/gmt2001/twitch/cache/ViewerCache.java#L181) | Watchtime callback adds 60 seconds to cached chatters every minute; offline counting is configurable. Viewer cache refreshes every two minutes and follows Helix chatter pagination to exhaustion. Time records in the inspected callback use lowercase login names. | We also sample chatters, but fetch pages within an earning window, identify viewers by numeric ID and retain durable cursor/pending work. Our window identities, leases and transactional replay protection address failures the inspected timer callback does not explicitly handle. That is an architectural difference, not a measured reliability victory over PhantomBot. |

[Twitch's Get Chatters contract](https://dev.twitch.tv/docs/api/reference/#get-chatters) confirms that the list describes connected chat users, updates with delay and can change while pagination proceeds. It supports at most 1,000 results per page and requires the proper moderator authorization. Our bot-token/moderator routing, page size and explicit pagination match that contract. Neither those systems nor our chatter-based design establish continuous video playback.

[Twitch's rate-limit guidance](https://dev.twitch.tv/docs/api/guide/#twitch-rate-limits) separates app-token capacity from user-token capacity, with user limits applying per client ID and user. It directs clients to use the reset header after a 429. Our shared quota charging and separate app/bot Valkey reset state follow that guidance, with timing defects identified below.

## Findings to fix, in priority order

### 1. P1 — Order account state within an incarnation

[Projection admission writes](../../internal/projection/valkey.go) compare account creation identity and deletion state, then overwrite active/banned flags without a source state revision. [Reprojection](../../app/db/users/repository/users.go) queries a batch of 500 users before publishing their saved snapshots individually.

A pause or ban can commit and publish after that query but before the older row is published. The older active/unbanned snapshot then arrives last, reopens admission and becomes a new accepted generation. Strict NATS delivery ordering cannot help because the stale snapshot is physically published later. Two reviewers independently traced this complete path; it is not a hypothetical bus reordering problem.

Add a source-owned monotonic user-state revision, include it in ordinary events, reprojection and RPC/hydration replies, and atomically reject older revisions in every user projection writer. Acceptance: pause/ban during reprojection and return an older hydration reply afterward; admission must remain closed.

### 2. P1 during rollout — Fence unknown-incarnation deletes

The [projector's legacy deletion fallback](../../app/projector/projector.go) deletes the current projection. The [SQL fallback](../../app/db/loyalty/repository/watchtime.go) treats an unstamped deletion as applying to the current account. A delayed pre-upgrade deletion can therefore remove a recreated account even though stamped old deletions are correctly rejected.

Reject or quarantine unknown-incarnation deletes once a positive current incarnation exists. Alternatively, a verified old-publisher/message drain is a strict deployment prerequisite; the documented rollout barrier is not an implementation fence. Acceptance: deliver a legacy zero-incarnation deletion after recreation and preserve new state/balances.

### 3. P2 — Revalidate after acquiring credentials and cancel token waits

[HTTP attempt admission](../../app/twitch/outgress/internal/twitch/client.go) occurs before token acquisition. A pause or shared cooldown established during a slow refresh can invalidate that decision before HTTP is sent. Revalidate immediately before sending, and charge the actual attempt once credentials are ready.

[Token refresh singleflight](../../app/twitch/outgress/internal/twitch/token.go) uses blocking `Group.Do`, so a caller joining another refresh cannot cancel its wait. A temporary overlay test reproduced a 10 ms caller remaining blocked after 40 ms. Use a context-selectable singleflight result. Acceptance: revoke during a blocked refresh and expire a second caller's deadline; neither should issue HTTP or occupy the worker past its cancellation.

### 4. P2 — Publish provider cooldown at response headers

[The client recognizes a 429 then drains the body](../../app/twitch/outgress/internal/twitch/client.go); [the RPC publishes the reset](../../app/twitch/outgress/rpc/chatters.go) only after the client returns. A slow body delays protection while another tenant/replica can continue sending against that exhausted token.

A temporary overlay test using the actual client and a controlled HTTP transport reproduced the second HTTP call before the first 429 body finished. Publish reset feedback before draining, and bound cleanup independently. Acceptance: block a 429 response body and ensure another tenant is already denied without consuming quota.

### 5. P2 — Bound unsampled window age and reconcile actual stream identity

[Retries retain the same cursor/window indefinitely](../../app/twitch/sesame/engine/loyalty_schedule.go). After a long outage, a newly fetched page can be combined with pages credited hours earlier under the old window identity. Earlier viewers then collide with old viewer/window dedup entries.

Finish immutable saved work, but expire unfetched cursor progress after a defined collection period and start a current window. Do not discard already committed credits or invent missed attendance. Acceptance: recover after a two-hour interruption following the first accepted page.

The [live check returns only a boolean](../../app/twitch/outgress/rpc/chatters.go). Missing both offline and subsequent online events can preserve the previous local session into a new Twitch broadcast. Return actual stream ID or started-at metadata and reconcile the session when it changes. These are sampling/session precision gaps, not demonstrated cross-tenant leakage.

### 6. P2 — Bound replay-ledger storage and prove capacity

[Viewer/window dedup rows](../../app/db/loyalty/repository/watchtime.go) have no pruning implementation. As an illustrative upper-bound workload, 100,000 viewers continuously present in live channels for 24 hours produce 28.8 million viewer/window rows per day at five-minute intervals. Retention needs finalized-window and replay watermarks that also reject late work; deleting dedup rows by arbitrary TTL would weaken replay safety.

The watch share sustains about 200 chatter attempts/minute. Ignoring retries and other constraints, required rate is `sum(ceil(chatters_per_channel / 1000)) / 5` per minute. That supports about 1,000 single-page live channels at five-minute cadence; fewer with multiple pages, failures or additional overhead. This is a budget calculation, not a tested service capacity.

Add mixed-size multi-tenant load tests and metrics for due age, collection age, queue delay, skipped intervals, quota rejection, oldest outbox age and SQL commit latency. Measure earnings coverage and small-channel progress under sustained large listings. The bounded workers and fair page yielding are useful safeguards; they are not throughput evidence.

### 7. Release verification — Exercise production MySQL and infrastructure failure

Existing posting tests use SQLite; recording-driver flush tests cannot establish MySQL locking behavior. Test separate production-MySQL connections racing duplicate delivery, posting, legacy flush, deletion and recreation. Include full 1,000-viewer pages within the consumer's commit timeout.

Run replica/process replacement, Valkey failover and verified retained-storage restore exercises. The storage migration, canonical account/config bootstrap and nonzero AOF/failover loss window remain documented in the [rollout guide](../../docs/operations/watchtime-rollout.md). Public commercial docs provide no comparable infrastructure measurements, so a better-than-StreamElements claim needs our own operational evidence first.

## What survived this review

The reviewers found no new immediate duplicate-credit or stale-worker mutation defect in the scheduler/outbox path. Atomic primary-Valkey admission and lease checks, immutable pending payloads, SQL operation and viewer/window dedup committed with credits, acknowledgment after commit, numeric tenant/viewer identity and stamped account-recreation fences remain strengths.

The previous touched-package, targeted real-Valkey and race checks establish those tested behaviors. The original second pass added temporary request-path reproductions and static cross-review; the implementation follow-up adds permanent regressions and fixes. Neither pass ran live Twitch calls or production infrastructure. Commercial comparisons use first-party documentation; PhantomBot comparison uses pinned primary source.
