# Sesame cold-message latency investigation

Investigated on 2026-09-26 against live Kubernetes logs, Valkey trial counters,
New Relic spans/transactions (account 3823179), and the current working tree.
The investigation below describes the pre-change behavior. The latency fixes
are implemented in this PR. Production was only inspected with read-only
operations; no deployment or production configuration change is included.

## Implemented fixes and validation

Three GPT-6 Sol agents implemented the changes, followed by one GPT-6 Astra
review. Astra found no actionable correctness issues in the scoped changes.

- Plain chat resolves locale only when a handler needs localized output.
  EmotePlay milestone replies retain localization. A resolved default locale
  is recorded explicitly so an empty string cannot cause repeated lookups.
  Trial provenance selects the default locale without an account lookup.
- The optional `projection.Client.LoadChannel` overlaps independent cold user
  and module loads for paths that predictably need both. Cached and single-miss
  paths use no extra goroutine. Existing Reader implementations, singleflight,
  separate invalidation, and user/module failure policies remain supported.
- Current main already reports trial counters asynchronously. The PR retains
  that bounded reporter, weighted message counts, and nanosecond metrics.
  Processing duration is captured before outcome reporting, and trial identity
  is attached before early branches.
- Projector starts go-live settings refresh after the durable live write and
  invalidation, before the three synchronous loyalty baseline reads. The
  baseline's existing all-or-none behavior is retained.
- Module projection reads return only `module:` fields and the completeness
  marker through atomic read-only `EVAL_RO`. Unknown-command/NOPERM errors
  retain the atomic HGETALL compatibility path. User datastore spans now say
  HMGET; fixed load-source attributes distinguish cold load sources.
- Sesame precompiles the exact envelope/output/projection codecs before
  consumption, including anonymous output payload builders. It also warms
  the existing node-local read pool with 512 read-only keyed HGET probes,
  eight workers and a three-second deadline. Failures warn and serving proceeds.

Validation used the CI-pinned Go 1.26.5 toolchain rather than the installed
Go 1.27.1, which is outside Sonic's supported version range:

- The combined race suite passed for `./app/twitch/sesame/...`,
  `./app/projector/...`, `./internal/projection`, `./pkg/cache`, and
  `./pkg/valkey`, including real socket fixtures. Tests cover deterministic
  cold-load overlap, singleflight, independent invalidation, fallback policies,
  module payload filtering/fencing, lazy localization, trial reporting,
  startup codecs, and bounded warmup/cancellation.
- The clean PR branch passes repository-wide `go test -race ./...`. The
  original investigation checkout had unrelated JSON-import guard failures;
  they are absent on current main and are not part of this PR.
- Local CodeScene delta analysis reports no new findings after refactoring the
  flagged functions. Astra reviewed the final port and found no remaining
  actionable issues.
- The exact module-filtering Lua script executed successfully against live
  Valkey on an absent reserved key and an existing settings hash. Replies were
  valid empty/marker snapshots containing only allowed fields. No data changed.
- `git diff --check` passed for the tracked files touched by these fixes.
- Local hot-cache benchmarks measured separate loads at about 294 ns/op,
  56 B and four allocations versus combined loads at about 322 ns/op, 88 B
  and six allocations. Plain-chat processing measured 798–827 ns/op and
  13 allocations. These are local implementation measurements, not a
  before/after production-latency comparison.

Accepted limits: a permitted baked trigger may still prefetch locale before
module/live/cooldown gates reject it; this retains some lookup overhead from
the old chat path. Lua narrows response bytes but still reads the complete
hash inside the server. Connection warming is probabilistic because the
library cannot target multiplex slots through its public API. Raw nested
module/event bodies can still compile their particular codecs on first use.
Trial receivers still do not subscribe to live events; trial first-chat
processing now avoids the absent-account locale request directly.

The observed 20–24 ms samples have not been remeasured with these changes in
production. Compare cold channels, expired local caches, and process startup
separately after deployment, using the added source/reporting trace attributes.

## Follow-up: hydration must not start offline watch time

Settings hydration writes account/module/command sections; it never creates
the worker's versioned `live:<id>` key. Watch admission and award enqueue
already require that key on the primary. The projector's `settings:<id>.live`
field alone is insufficient for watch admission.

The audit found a scheduling race: the live key could be removed after
`Capture` (or the recovery read) but before the separate arm Lua script.
`loyaltyArmScript` now checks the current live key against the admitted
event/recovery version atomically, before any schedule/claim/due mutations.
An absent or superseded live key rejects arming. Existing active windows
remain stable through successful Twitch live rechecks.

Real isolated Valkey race tests passed for watch scheduling/live versions,
watch admission/outbox, and full settings hydration. Coverage includes
hydrated offline accounts, delayed hydration finishing after offline, stale
award rejection, precise offline/newer-online transitions between admission
and arm, and legitimate online/reconfirmation behavior. A broader real-Valkey engine
run also exposed three unrelated existing EmotePlay fixture failures; its
fixtures reuse broadcaster IDs across tests without clearing their keys.
The scoped watchtime tests pass. Production remains unchanged. One Astra review found no production issue;
its test cleanup feedback was fixed by joining delayed hydration before
fixture cleanup. Astra confirmed the follow-up fixes and additional coverage,
with no remaining actionable findings in this scope.

## Integration with current main

The original shared checkout was an older snapshot with unrelated local work.
The PR ports only these fixes onto current main and preserves its moderation,
trial promotion, weighted message counts, and localized output behavior.
Current main already aggregates trial counters asynchronously, so the PR
retains that reporter and its nanosecond metrics instead of adding the older
synchronous DoMulti implementation. Trial duration is still captured before
outcome reporting. Lazy locale resolution also covers matching Personality
and Triggers replies, and output locale is read from the current message
context when it emits.

Validation was rerun independently on the clean branch. The full race suite,
focused real-Valkey regressions, CodeScene analysis, and final Astra review
passed. Historical measurements below remain pre-deployment observations.

## Scope: first message for a newly live or quiet/offline channel

The user clarified that the target is a channel's first message, rather than
the first message after a worker restart. The primary fixes for that case are
foreground loading and channel prewarming. Decoder and connection startup
costs below are separate contributing cases, not an explanation for every
first-channel message.

- Sesame's user/module/command caches last only 30–33 seconds and are local to
  each replica. A quiet channel's first message therefore reloads them even if
  shared Valkey is warm. Warming Valkey does not populate these local caches.
- On a registered channel's `stream.online`, Projector refreshes the shared
  settings projection asynchronously with 24-hour retention. Offline events
  do not run that refresh. Query hydration defaults to two hours, and existing
  longer TTLs are not shortened.
- **Go-live hydration starts late:** `HandleStreamEvent` calls
  `snapshotCounterBaseline` before `RefreshAsync`. The baseline performs three
  sequential loyalty RPCs, each with a two-second timeout. Start settings
  hydration before that independent baseline work; the baseline can retain its
  current synchronous all-or-none behavior. This removes a concrete delay in
  prewarming, although its wall-clock contribution to the reported 20 ms has
  not been measured.
- The trial receiver subscribes only to `channel.chat.message`, and dispatches
  only those notifications. It supplies no `stream.online` event to trigger
  settings refresh when a trial streamer goes live. Live and offline trial
  channels therefore enter the same first-chat projection path.
- An unregistered trial account has no user projection to warm successfully.
  Repeating its users-service lookup after local-cache expiry is unnecessary;
  the resulting default locale/tier is already known from trial provenance.

Stable-process traces support this separately from startup: trace
`00a1375d29291e65b10b2e28c0d7a81a` took 10.031 ms, of which 8.504 ms was the
users RPC, 0.637 ms was the preceding user settings read, and only 0.210 ms was
decoding. The module cache was already warm (0.066 ms stage). Trace
`584f6f5c4997de0321fae53027d5c13f` took 9.176 ms, with 1.652 ms module loading
followed by 0.531 ms user settings loading and 6.318 ms users RPC. These confirm
that a cold account lookup dominates even when JSON compilation is not involved.

## Measured evidence

A sampled chat-message trace at 23:07:58 UTC, on
`sesame-c79d9cfb-zkh9h`, took **24.125 ms** in `message.process`:

| Sequential work | Time |
| --- | ---: |
| Envelope decoding | 0.114 ms |
| Module projection stage | 10.502 ms |
| User settings read inside engine stage | 7.672 ms |
| Users RPC inside engine stage | 5.307 ms |
| Other engine/framework work | about 0.53 ms |

Trace ID: `71f0202c93f185e520df7f3d3a91a475`; broadcaster 411377640.
The module-stage Valkey call itself was 10.168 ms. The user-stage RPC's
MySQL query was 2.260 ms **inside** the 5.307 ms RPC, not an additional cost.
Consumer queue wait was only 0.066 ms. This demonstrates serial projection
loading; it is a message-processing trace, not a measured Twitch reply round trip.

For broadcaster 40934651 (Ludwig), trace
`1c06054f52b83c2007f0e7c21c9a26d5` at 23:05:32 UTC took **36.309 ms**:
16.046 ms decode, 5.536 ms module projection, and 14.668 ms engine.
The engine included another 6.600 ms settings read and a 4.826 ms users RPC.
Other pods' initial messages had 25.814 and 26.134 ms decoding spans.
Those events occurred within seconds of the pods becoming ready during a rollout.

In a 30-minute sampled span query, the warm median was approximately
0.212 ms for the Sesame transaction and 0.355 ms for instrumented settings
reads. The sample included 254 message-processing spans and 32 settings-read
spans; these are sampled observations, not exhaustive traffic percentiles.

At the trial-counter snapshot, Ludwig had 8 processing samples totaling 45 ms
(5.625 ms average), and caseoh_ had 6 totaling 25 ms (4.167 ms average).
Both settings hashes had complete module/command markers but no `status` or
`active` fields. Projector logs at 23:07:28 and 23:07:30 UTC explicitly reported
`hydration: section failed`, section `users`, `user account not found` for those
IDs. The counter sample is too small to establish a first-reply percentile.

The active trial set contained 17 IDs at inspection time; the four-channel
limit mentioned in the older trial specification is not the observed live set.

## Causes and opportunities

1. **The foreground reads are sequential.** `Pipeline.Process` waits for
   `tracedModuleViews` before running stages. `runStages` then calls
   `ensureLocale` for events with handlers, which loads `User` even on plain chat.
   `runBaked` also loads `User` for command replies. Independent cold module/user
   loads can overlap, or the module hash read can supply user fields too.
   Keep the cached path synchronous and cheap; do not add a goroutine per hot
   message. Preserve separate cache invalidation and existing failure policies.
   With the measured 24 ms trace, ideal overlap alone would reduce these stages
   toward 13–14 ms; that is an estimate, not a verified improvement.

2. **Read connections initialize lazily.** `pkg/valkey/client.go` configures
   `PipelineMultiplex: 5`, meaning 32 connections. In valkey-go v1.0.77,
   `newSingleClient` calls `mux.Dial`, which opens only connection zero.
   `mux.pipeline` chooses a connection through `slotfn`, which selects randomly
   for keyed commands, and `mux._pipe` opens unused connections on demand.
   Consecutive reads can therefore each encounter a new TLS/authentication
   connection. This is a strong explanation for the initial 5–11 ms reads,
   compared with the sub-millisecond warm median. Existing telemetry does not
   isolate TLS/dial time, so the exact share is not measured.
   Warm the read connections with bounded read-only work before consuming.
   Repeated `PING` alone is insufficient: unkeyed commands select connection zero.
   Retain the pool size until an A/B test justifies reducing it; its throughput
   benefit is documented in the current code.

3. **JSON codec compilation runs on the first message.** Sesame has no startup
   call to `codec.Pretouch`; Sonic compiles each type's codec on first use.
   The large first decode spans are consistent with this startup cost, though
   they include any scheduler time as well. Precompile the envelope and hot
   output/projection reply types before consumers start. Outgress already has
   `worker.PrepareJSON` as an implementation precedent.

4. **Trial accounts remain permanent shared user-projection misses.** An empty
   projected status triggers the users RPC. Its not-found result falls back to
   standard in Sesame's 30–33 second local cache, then expires independently on
   each pod. The users repository does not cache failed SQL lookups. The
   Projector also retries the absent account during background hydration.
   A trial envelope can use the default locale without querying a nonexistent
   account, or the projection contract can represent a short-lived, invalidated
   negative account result. Do not fabricate a registered account or give a
   missing account a positive status marker. Registration must invalidate any
   negative result, and trial provenance must continue to block output.

5. **Trial bookkeeping adds its own synchronous network trips.** Current
   `Pipeline.Process` increments `decoded`, then `processTrial` increments
   `processed` (or failure/retry fields), `latency_samples`, and
   `latency_total_ms` through separate `Do(HINCRBY)` calls. That is four writes
   for an ordinary successful trial envelope. Batch these operations, or use a
   bounded counter reporter with explicit shutdown/failure behavior. Capture
   processing duration before persisting counters. The current trial timer
   excludes `decoded` but includes the outcome write and the sample-count write;
   the outer `message.process` span includes all of them. This observation
   comes from current source; the selected historical traces do not establish
   an exact per-write contribution.

## Concurrency already present

Projector's `Hydrator.fill` launches user, module, and command sections in
parallel. Each section independently fetches/retries/writes. Background
hydration is not the serial bottleneck demonstrated above. Cache singleflight
also collapses concurrent misses for the **same** entry; it does not overlap
different user/module cache loads within a message.

## Suggested implementation order

1. Avoid absent-account locale reads on trial envelopes and batch trial counters.
2. Launch registered-channel go-live hydration before the loyalty baseline RPCs.
3. Overlap independent foreground cold loads, or reuse one settings snapshot;
   load locale only for paths that actually need localized output.
4. Precompile Sesame codecs and warm read connections as separate startup fixes.
5. If large registered-channel hashes remain slow, narrow the module read:
   `GetModules` currently downloads the entire settings hash, including command
   and fetch bodies, then discards everything outside `module:` fields.

Validate each change on cold processes and expired local caches separately.
Record module/user load source, connection setup, processing, counter-reporting,
and output-publish durations. Current user `HMGET` is incorrectly labeled
`HGETALL` by the datastore segment; fix that attribution during instrumentation.
Trial origin/broadcaster attributes also need to be attached before the early
`processTrial` branch to make new trial traces directly filterable.
