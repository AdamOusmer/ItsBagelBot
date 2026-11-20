# Giveaways, raffles and loyalty: decoupled design

Date: 2026-11-19
Status: Proposed. Design only; nothing here is implemented. Open decisions are in section 15.

## 1. Outcome

Three separate tools on one reliable engine:

- **Giveaway**: a real prize (a game key, a headset). Free to enter, explicit winner count, participation rules, presence check, per-slot reroll, stored winners. Never pays points.
- **Raffle**: a points prize. Lives under Loyalty next to Gamble and Duels. Explicit or predictable winner count, exactly-once payout. Nothing is stored once it finishes; the balance change is the only trace.
- **Loyalty**: the bank. Earns, stores and spends points and watch time, reports whether accrual is healthy, and can **freeze** its public standings and eligibility snapshot for up to 30 minutes while live balances keep counting.

A streamer runs any of them from one page or one chat line, and no round ever mixes an item with points.

## 2. What is wrong today

Evidence from `origin/main` at 54b713b74. `engine/`, `module/` and `modules/` are under `app/twitch/sesame/`; `repo/` is `app/db/loyalty/repository/`.

Coupling
- One module, one config struct, one Valkey keyset and one winner line carry both the points prize and the custom item. Round state stores the points prize; the item text comes from current config at announcement (`engine/raffle_config.go:24`, `engine/raffle_valkey.go:23,55-69`, `engine/raffle_announce.go:56-79`). A game key giveaway also pays the default 1,000 points unless the streamer zeroes `prizePoints` (`engine/raffle_config.go:16-18,58-64`, `web/kit/lib/catalog/raffle.ts:12-20`).
- Claim settings and prize text are read live at draw and at `!claim`, so editing them mid-round changes advertised terms (`modules/raffle.go:61-68,332-345`, `engine/raffle_announce.go:149-155`).
- Loyalty off at open silently zeroes the prize behind the normal opening line (`modules/raffle.go:184-204`); off at draw skips credits and marks `loyalty_off` (`engine/raffle_payout.go:87-94`).
- Raffle is a top-level Play module (`web/kit/lib/catalog/raffle.ts:6-16`) while Gamble and Duels are Loyalty children that link out to their own module pages (`web/kit/lib/catalog/games.ts:15,59`, `web/dashboard/src/lib/components/loyalty/LoyaltyGameRow.svelte:29-30`). One points raffle touches three pages.

Multi-raffle
- Automatic winner brackets fall as entrants rise: 10 entrants give 5 winners and 11 give 2; 20 give 5 and 21 give 4; 50 give 10 and 51 give 6; 200 give 25 and 201 give 10 (`engine/raffle_mechanics.go:31-46`, pinned by `engine/raffle_valkey_test.go:302-317`).
- `splitPrize(3, 10)` pays `[1 1 1 0 0 0 0 0 0 0]`; all ten are announced, and the line quotes the first winner's share for everyone (`engine/raffle_payout.go:72-85`, `engine/raffle_announce.go:75-79`).
- The winner count cannot be set at open, only overridden at `!raffle draw N` (`modules/raffle_args.go:15-28`, `modules/raffle.go:270-275`).

Reliability
- Teardown removes the due entry before payout and keeps no per-winner payout record, so a crash mid-payout strands credits (`engine/raffle_valkey.go:317-332`, `pkg/valkey/receipt_sessions.go:148-156`).
- `balance.add` carries no idempotency key; a lost reply plus the retry loop can pay twice (`engine/raffle_payout.go:105-113`, `engine/loyalty_rpc.go:56-65`, `internal/domain/rpc/loyalty/loyalty.go:13-28`).
- The 10-second draw lock can lapse during credit retries (`engine/raffle_valkey.go:38,235-237`).
- An old `recordPayout` can overwrite a newer result, and claims survive into the next draw (`engine/raffle_valkey.go:335-340`, `pkg/valkey/outcome_claim.go:23-45`).
- Rounds are identified by broadcaster only; a delayed expiry can act on a reopened round, and a nil draw under lock contention drops the due entry (`engine/raffle_valkey.go:352-374`, `engine/raffle_announce.go:132-142`).
- A join between pool read and snapshot is stored but never drawable (`engine/raffle_valkey.go:177-191,247-250,327-329`).

Missing
- No participation rules: `!join` only needs an id and a login (`modules/raffle.go:218-228`).
- No per-channel accrual health; only a service-wide outbox age check that fails above five minutes (`internal/watchtime/consumer.go:180-195`). Per-channel tick state arms and disarms (`pkg/valkey/window_schedule.go:34-44`) and stages pages (`:64-92`) but records no last-page time.
- No freeze. A balance is one mutable row per viewer with no ledger (`app/db/loyalty/ent/schema/balance.go:19-45`), so an as-of view must be copied when it is taken.
- Gamble cooldowns are keyed by login, not Twitch id (`modules/gamble.go:21-23,108,216-222`).

## 3. Domain model

| Term | Meaning |
| --- | --- |
| Round | One open-then-drawn session with a stable `round_id`. Kind `giveaway` or `raffle`. |
| Terms | Prize, winner count, rules, claim window and weights, copied into the round at open and never re-read from settings. |
| Entry | One viewer id admitted to a round, with its weight and the result of the join-time rules. |
| Slot | One winner position. Carries a generation number and a state (section 5). |
| Rule set | Participation rules in the round's terms. |
| Winner record | One stored row per giveaway winner: round id, prize, time, viewer id, login, claim and delivery state. Nothing else about a round is stored. Raffles store nothing. |
| Freeze | A channel-wide copy of standings, at most 30 minutes, read by public standings and eligibility while live balances keep changing. |
| Accrual health | Whether this channel's watch accrual is running now. |

Model rules
- One **joinable** round per channel across both kinds, so `!join` always has one meaning. Rounds that are drawing, claiming or paying coexist with a new joinable round. Opening while another round is joinable is refused: "A giveaway is already open. Draw or cancel it first."
- A giveaway never pays points; a raffle never carries item text.
- Admission sticks: rules checked at `!join` are not checked again. Rules that run at draw are checked once per drawn viewer.
- Terms change only by an explicit mod action, and each change is recorded on the round while it runs: Close now, Add 1 minute (moves `closes_at`, never past the freeze end when the rules read the freeze), `!raffle draw N` (resolves the winner count), and D5's skip-rule decision. `auto` is stored as a policy and resolved to a count at close.

## 4. Boundaries and ownership

| Concern | Owner |
| --- | --- |
| Round lifecycle, entries, selection, slots, claims, rerolls | Sesame `engine/draw` (new package) over new `pkg/valkey` operations |
| Shared viewer commands `!join`, `!claim`, `!check`, `!winner` | One Sesame command owner registered before Queue. `!join` claims the command only while a round (new or legacy) is joinable, otherwise it falls through to Queue as today (`modules/all.go:29-30`, `engine/dispatch.go:73-90`). `!claim` and `!winner` keep working for settling rounds after their module is turned off |
| Giveaway mod commands and settings | Sesame `modules/giveaway.go`, new module `giveaway` (Play) |
| Raffle mod commands and settings | Sesame `modules/raffle.go`, re-parented to Loyalty |
| Participation rules | Sesame `engine/eligibility` (new package) |
| Giveaway winner records | Modules service (`bagel_modules`), like quote books (D1) |
| Raffle payout dedup markers (short-lived) | Loyalty service (`bagel_loyalty`) |
| Freeze state and snapshot | Loyalty service (`bagel_loyalty`) |
| Followage and account-age lookups | Outgress, charged to its existing Helix buckets |
| Settings | Modules service module rows (`giveaway` new, `raffle` kept) |

No code in `app/` calls Valkey or MySQL outside its shared package; every new operation is added to `pkg/valkey` or the owning repository and declared through the existing builders.

## 5. Draw engine (`engine/draw`)

Keys (all through `pkg/valkey`)

```
draw:joinable:<channel>      round_id of the one joinable round
draw:round:<round_id>        hash: kind, terms, phase, opened_at, closes_at
draw:entries:<round_id>      zset viewer_id -> weight
draw:labels:<round_id>       hash viewer_id -> login, display only
draw:refused:<round_id>      hash viewer_id -> join-time rule refusal for current non-entrants
draw:slots:<round_id>        hash slot_n -> {generation, viewer_id, state, deadline, reason}
draw:claimable:<channel>     hash viewer_id -> round_id:slot_n:generation
draw:due                     zset round_id -> next action time
```

Round phases: `open -> closing -> settling -> done`, or `cancelled`. Each transition is a compare-and-set on `phase` in one script. Every caller (chat, dashboard, timer, sweep) goes through the same transition, and the loser gets "already drawing" or nothing. A stale timer for an older round fails the round-id check and does nothing.

Close
- `open -> closing` and the release of `draw:joinable` happen in one script, and join checks `phase == open` in the script that adds the entry. Every stored entry is drawable.
- Cancel is allowed in `open` and `closing`. In `settling`, a mod can only end unclaimed slots.

Slot states

```
pending -> verifying -> won -> claimed            (giveaway with claim window)
                           -> no_show -> pending (generation + 1, on reroll)
                           -> paid | unpaid       (raffle)
        -> unfilled                               (pool exhausted)
```

- Pick: Go generates the random value with `crypto/rand` over the remaining eligible weight, excluding every viewer already drawn in the round, and a `pkg/valkey` compare-and-set on slot state and generation moves the slot to `verifying` with the candidate. The pool cannot change after `closing`, so the phase is the pool revision.
- Verify: draw-time rules run outside the script (RPC). A pass moves `verifying -> won` by compare-and-set on slot and generation; a fail records the reason and returns the slot to `pending`; a lookup error leaves it in `verifying` with a due retry (bounded by D5).
- Announce: giveaway winners are announced when their slots reach `won`; raffle winners after their slots reach `paid`. Slots finishing in the same step share one line. If verification or payout is still running after 10 seconds, one "verifying winners" line is posted so chat is never silent.
- Weighted selection is sequential: each pick is proportional to the remaining eligible weight. For one winner this is exactly weight over total eligible weight.
- The due zset is the recovery path for every phase: a crashed close, a slot stuck in `verifying`, an expired claim window and an unpaid slot are all due actions retried until the phase completes. Each due action carries the round id and the slot generation it expects. For a giveaway, the winner rows are written to Modules first and the due entry is removed after that write. At `done` or `cancelled` every `draw:*` key of the round is deleted, for both kinds.
- Rounds keep settling if their module is turned off or the stream goes offline mid-round. Turning a module off only stops new rounds.

Why verify only drawn viewers
- Drawing from all entrants and rejecting ineligible candidates is uniform over the eligible set, because each accepted pick is conditioned only on eligibility. Helix lookups scale with candidates drawn, not with entrants; the real count depends on the ineligible share and cache hits.

Winner count
- Giveaway: the stock, set at open (default 1). Policy maximum 100 (section 16).
- Raffle: set at open; default 1 for `!raffle` and `!sraffle`, 3 for `!mraffle`. Setting `auto` uses `ceil(entrants / 10)`, bounded by the policy maximum and by the prize so each winner gets at least 1 point. It never decreases as entrants rise. The old brackets are removed.

Presence check and reroll (giveaway)
- Claim window: off, or 30 seconds to 15 minutes; default 2 minutes. Slots announced in the same line share one deadline; a rerolled slot gets a fresh window. With the window off, `won` is final.
- `!claim` resolves the viewer through `draw:claimable` and claims every open slot that viewer holds, checking deadline, slot and generation in one script. `!reroll` targets the newest settling giveaway; `!winner` shows the newest round still settling, otherwise the last stored giveaway winners.
- At the deadline the unclaimed slots of that line become `no_show` together. With "reroll no-shows" on, they return to `pending` together and the replacements share one line; otherwise the panel shows Reroll, and `!reroll` rerolls every no-show (`!reroll 2` for one slot).
- A round is `done` when every slot is `claimed`, `paid`, `unpaid`, `unfilled`, ended by a mod, or `won` with the claim window off.

Fairness record
- While a round runs, each slot keeps every candidate with its generation and rejection reason, shown on the live panel. None of it is stored after the round ends. Selection uses `crypto/rand` as today (`engine/raffle_mechanics.go:48-81`). A seed commit-reveal scheme was considered and dropped: the server would still hold the seed and controls eligibility, so it would add ceremony without a real guarantee.

## 6. Giveaway

Settings (`giveaway` module row)
- Default prize name (floor-checked at save and at open), winners, entry window, claim window, reroll no-shows.
- Rule presets, for example "Regulars" (7 day follow, 2 h watch time) or "Subs only". A round copies its preset at open.
- Subscriber luck: optional multiplier 1x to 3x (default off, D2) for viewers with a subscriber or founder badge. Stored on each entry.
- Skip recent winners: exclude anyone who won a giveaway in this channel within N days (default off). Checked at draw against the stored winner rows.
- Freeze standings while open: default on when the preset has a points or watch time rule (section 11).
- Rejection line: when `!join` fails a join-time rule, reply once per viewer per round with the reason (default on). A standings lookup failure prompts a retry once per viewer per round and leaves the refusal count unchanged. Successful joins stay silent.

Chat (mods)
- `!giveaway start [preset] [prize...]`: the first word is a preset only when it names a saved preset; otherwise all the text is the prize and the default preset applies. No prize text uses the default prize. Examples `!giveaway start regulars Steam Deck`, `!giveaway start Steam Deck`.
- `!giveaway draw` closes now; `!giveaway cancel`; `!reroll [slot]`.

Chat (viewers)
- `!join` enters the joinable round.
- `!claim` confirms a won slot within its window.
- `!check` (per-viewer cooldown): the viewer's status against the joinable round, for example "You need 2h watch time (you had 1h 20m at 20:14). Follow age is checked at the draw."

Opening line names the prize, winners, time and requirements: "Giveaway: Steam Deck, 3 winners, 3 min. Needs 7 days following and 2h watched. Type !join". Giveaway chat lines have their own toggles in the `giveaway` row; the migrated raffle toggles apply to raffles only.

Dashboard live panel
- Start: preset, prize, winners, window. Before Start it shows how many standings already meet the points and watch rules (one count on the snapshot or live table).
- While open: entries, rejected at join, time left, Close now, Add 1 minute.
- After close: one row per slot with winner, claim countdown, state, rejection reasons of replaced candidates, Reroll, and Mark delivered. Prize keys are never stored.
- A banner when the freeze ends before the draw has started (a delayed close).

Winner history
- Only winners are stored, one Modules row each, written when the round reaches `done`. Mark delivered updates that row. Entrants, rejected candidates and rules are not stored.
- The channel's public page lists past giveaway winners: prize, time, winner.

## 7. Raffle (points, under Loyalty)

Chat stays compatible: `!raffle [amount] [seconds]`, `!sraffle`, `!mraffle [amount] [seconds] [winners]`, `!raffle draw [winners]`, `!cancelraffle`, `!join`, `!winner`. Raffles never use `!claim`.

- Catalog `parent: 'loyalty'`; settings move to the Loyalty Games tab. Opening with Loyalty off is refused with one line instead of a silent zero prize.
- Even split with integer floor: every winner gets the same announced amount and the remainder is not minted.
- A raffle opened while Loyalty was on pays its terms even if Loyalty is turned off before the draw.
- Payout per slot through `balance.add` with key `raffle:<round_id>:<slot_n>`:
  - The loyalty service stores the key with a payload hash and the original reply in the credit transaction, under the account fence. A replay with the same payload returns the stored reply; a different payload is refused.
  - Transient errors leave the slot `won` with a due retry. Permanent errors (invalid input, overflow, retired account) move it to `unpaid`.
  - The winner line is posted after payout and states the paid amount: "@a, @b and @c each won 1,666 points".
  - While the raffle is settling, `!winner` and the dashboard list unpaid slots by name. Once every slot is `paid` or `unpaid` the round's keys are deleted and nothing remains.
  - A slot stops retrying 24 hours after the draw and becomes `unpaid`. The dedup marker is only the key and payload hash, written in the credit transaction so a retry cannot pay twice. It is deleted 24 hours after the draw, once no retry can arrive, with the existing retention pattern (`repo/counter_retention.go:31`). It is not a raffle record.
  - The round records the winner's account instance when the slot is drawn; a credit for a different instance (deleted and recreated account) is a permanent error.
  - Keyed credits use a new verb `balance.credit` that requires the key, so an old loyalty pod can never accept a keyed credit without deduplicating it. Loyalty ships first.

## 8. Participation rules (`engine/eligibility`)

| Rule | Source | When |
| --- | --- | --- |
| Role: everyone, subscribers, VIP or higher | Chat badges (`module/permission.go:19-50`) | At `!join` |
| Exclude the broadcaster and optionally mods from giveaways | Envelope | At `!join` |
| Minimum points | `balance.snapshot` (new, freeze-aware) | At `!join` |
| Minimum watch time (lifetime in channel) | Same read | At `!join` |
| Followers only, minimum follow age | `followage.get` (`engine/followage_rpc.go:15-21`) | At draw |
| Minimum account age | `accountage.get` (`engine/accountage_rpc.go:17-21`) | At draw |
| Skip recent winners | Modules giveaway winner rows | At draw |

- Join-time rules run in this order and stop at the first failure. The result is stored on the entry.
- The chat pipeline drops messages from the configured bot before command dispatch.
- Draw-time rules run per candidate (section 5). `!check` runs them for the asking viewer behind its cooldown.
- `balance.snapshot` reads the freeze copy when one is active and live balances otherwise. A viewer with no row reads as 0 points and 0 watch time. Frozen standings reads are cached in-process per Sesame replica through `pkg/cache`, keyed by channel and viewer. The loyalty freeze marker invalidates that channel's cache entries. Live reads bypass the cache, and cached replies expire within 30 seconds or at freeze expiry, whichever comes first.
- Followers are not in chat badges, so the followers rule runs at draw.
- `followage.get` and `accountage.get` start charging the existing bucket of the credential they use (bot for followers, app for users, `app/twitch/outgress/internal/twitch/client.go:326-352`) on every HTTP attempt. Throttling returns a typed retry time carried through the RPC reply and Sesame's decoder (`engine/cached_rpc.go:53-64`), separate from a missing scope, so verification reschedules instead of failing. Pattern: `app/twitch/outgress/rpc/chatters.go:429-467`. Requests carry Twitch ids only. No new caps.
- A rule on points or watch time requires Loyalty on and accrual not `stalled` at open (section 9).

## 9. Accrual health

Healthy means all of:
- Loyalty module on.
- This channel's tick state is active, has no recent failure, and committed a page within two tick intervals. Sesame owns this; the tick state in `pkg/valkey/window_schedule.go` gains a `last_page_at` field written at page commit.
- The loyalty watch outbox is draining service-wide (oldest entry under 5 minutes, the check already in `internal/watchtime/consumer.go:180-195`), exposed through a new `watch.status` verb. It is a backlog signal, not per channel.

States: `healthy`, `starting` (stream live, first page not yet due, at most one tick interval plus jitter), `offline` (not live: accrual paused, rules on points and watch time still work on stored values), `stalled` (any other failure). Opening a round with a points or watch rule is refused only when `stalled`, with the reason shown on the panel. The Loyalty page shows the same status line.

## 10. Freeze

Semantics
- A freeze pins public standings and `balance.snapshot` to the values copied at `as_of`. Live balances keep changing: watch awards, subs, cheers, games and mod adjustments commit as today.
- `as_of` is the start of the copy statement. A watch award is committed in chunks of up to 64 viewers (`repo/watchtime_posting.go:22-25,51-59`), so standings can differ by one award window between viewers. That is accepted and stated.
- At most 30 minutes from `as_of`; never extended.

Holders
- One freeze per channel, with a `freeze_id` and a set of holders: `mod` (with its own end time) and `round:<id>`.
- `!freeze [minutes]`, `!unfreeze` and the dashboard button manage the mod hold. A round with "freeze standings while open" adds its hold at open and releases it when it leaves `open`.
- A round opening during a mod freeze joins it; no second copy.
- The freeze ends when the last holder releases or at `expires_at`. A round releasing its hold leaves a mod hold running until the mod's end time. `!unfreeze` during a round's hold is refused: "A giveaway is using the frozen standings; drawing or cancelling it unfreezes."
- Every release is a compare-and-set on `freeze_id`, so a late release from an old round never ends a newer freeze.
- A round whose rules read the freeze cannot have an entry window longer than the freeze has left.
- If the loyalty freeze succeeds and the round open then fails, the orphan hold ends at `expires_at`. That bound is the cleanup.

Storage (loyalty service)
- `loyalty_freezes(user_id PK, freeze_id, as_of, expires_at, holders)`.
- `loyalty_freeze_balances(user_id, freeze_id, viewer_id, points, watch_seconds, login, viewer_name)`, natural key `(user_id, freeze_id, viewer_id)` registered in `MigrateSchema`, plus a rank index `(user_id, freeze_id, points DESC, viewer_id)` matching the live one (`app/db/loyalty/ent/schema/balance.go:44-45`, `repo/top_page.go:18,137`).
- The freeze verb copies with one `INSERT ... SELECT` and writes the freeze row in the same transaction, at READ COMMITTED (`pkg/db/provider.go:102`). Under READ COMMITTED the source read takes no row locks, so accrual is not blocked. The copy must never run inside a REPEATABLE READ transaction; a MySQL test pins this.
- The verb is idempotent by holder: a retry after a timeout returns the active freeze.
- Freeze, reset and account cleanup serialize on the channel's `loyalty_freezes` row, not on the account fence, so watch commits (which take the fence, `repo/watchtime.go:43-51`) never wait for a copy. Reset while frozen is refused. Account deletion and recreation lock the freeze row and delete snapshot rows (`repo/watchtime.go:226-258`).
- Old `freeze_id` rows are deleted in batches after the freeze ends (`repo/counter_retention.go:31` pattern).
- Before shipping, measure copy time, lock waits and replica lag on the largest channel under live accrual.

Why a full copy
- Holding accrual back and replaying it on unfreeze breaks the account fences, the retention floor, the overflow checks and the spend guard (`repo/watchtime.go:183-243`, `repo/watchtime_posting.go:148-158`, `repo/queries.go:98-105`). The copy leaves every write path untouched, which is what keeps counting exact.
- A copy-on-write pre-image avoids the bulk copy but touches every write path and makes frozen top lists a full scan. Revisit only if the measured copy time is a problem.

Readers
- Frozen: `top.get` (public page podium, paging and search; `!leaderboard`; dashboard top list) and `balance.snapshot`. Every reply carries `freeze_id`, `frozen_as_of` and `expires_at`, and one page load stays on one `freeze_id`. Search on the frozen path reads the snapshot and never reconciles logins into it (`repo/top_page.go:115-120` stays on the live path).
- Live: `balance.get`, `!points`, `!watchtime`, variables, automations, gamble, duel and transfer. A viewer always sees their real balance.
- Optional per-freeze flags: block `!points give`, and close point games, while frozen (both default off).

Caches and edge
- Freeze and release publish `ScopeLoyalty` (drops Sesame's top cache, `engine/loyalty_valkey.go:343-351,394-403`) and a freeze event. The dashboard server consumes the freeze event and schedules the leaderboard tag purge (`web/dashboard/src/lib/server/edge-purge.ts:139-142`), which covers chat-triggered freezes too.
- Natural expiry fires no event, so every cache clamps its TTL to `expires_at - now`, and the public page's edge TTL does the same.
- Even so, a change normally takes 10 to 70 seconds to reach the public page, longer on purge retries or edge errors (`web/dashboard/src/lib/server/edge-purge.ts:26-27,64-68`, `web/dashboard/src/hooks.server.ts:75-84,157-165`). The freeze button says so.

Public page copy: "Leaderboard paused at 20:14. Points and watch time are still counting."

## 11. Streamer and viewer flows

Dashboard
- **Loyalty**: Status (accrual health, freeze control), Earning (rates), Standings (top list, reset), **Games** with Raffle, Gamble and Duels settings inline. The gamble, duel and raffle module pages redirect there.
- **Giveaways** (Play): live panel, presets, past winners.

Item giveaway
1. Giveaways: preset "Regulars", prize "Elden Ring key", 3 winners, 3 minutes, Start. Or `!giveaway start regulars Elden Ring key`.
2. One opening line with prize and requirements. Viewers `!join`; a viewer under 2h watch time gets one reply saying so.
3. At close, candidates are verified; one line names the winners and the claim window.
4. Winners `!claim`. A no-show is rerolled with a fresh window. Mark delivered per row. The freeze ended at close.
5. The winners appear in past winners and on the public page.

Points raffle
1. `!mraffle 5k 60 3`. One opening line.
2. At close: draw, pay, one line "@a, @b and @c each won 1,666 points".

Viewer
- `!join` enters whatever is joinable; a refusal says why once. `!check` explains eligibility. `!points` is always live; the public board shows the frozen time.

## 12. Point games cleanup

- One amount grammar for every points input (gamble bets, duel stakes, raffle amounts): integers, `k` and `m` suffixes, `half`, `all`. One parser in `modules`, one table-driven test.
- Gamble cooldown keyed by viewer id.
- Every game result line offers the `{balance}` token.
- Optional "close games while frozen" (section 10).

## 13. Migration

- The `raffle` row keeps `prizePoints`, `durationSeconds` and its line toggles. `claimEnabled` and `claimPrize` seed the new `giveaway` row (claim window, default prize) and are no longer read by raffles.
- Rollout needs a mutual gate before the new engine opens anything. First a release where the legacy `Open` also refuses while `draw:joinable:<id>` exists (today it checks only its own deadline, `engine/raffle_valkey.go:154-174`). Then the release where new `Open` refuses while a legacy `raffle:state:<id>` exists and the shared `!join` routes to the legacy store while one is open. The legacy store keeps its due sweep until `raffle:due` is empty, then is removed.
- `!claim` and `!winner` read legacy results until they expire (24 hours). Legacy raffle results are not migrated.
- Recipe tokens: `{raffle:*}` keeps its names; giveaways add `{giveaway:prize}`, `{giveaway:count}`, `{giveaway:left}`.

## 14. Later

Custom entry keyword, giveaway overlay events on the desktop overlay lane, entry by channel point redemption, raffle ticket pots (D3), subscriber tier weights.

## 15. Open decisions

- D1: giveaway winner rows in Modules (`bagel_modules`). Recommended, alongside quote books.
- D2: subscriber luck in v1. Recommended, capped at 3x.
- D3: raffle ticket pots (points spent to enter). Recommended later: a debit in SQL plus an entry in Valkey needs its own replay-safe join, which v1 does not need.
- D4: `!check` and join refusals in chat (one line, deduplicated) or by whisper. Recommended chat; whispers need an extra scope and Twitch limits them hard.
- D5: how long a slot may stay in `verifying` on repeated lookup errors before a mod is asked to decide. Recommended 2 minutes, then the panel offers "skip rule" or "reroll".

## 16. Policy pins (one table in code)

| Pin | Value |
| --- | --- |
| Maximum winners per round | 100 |
| Giveaway default winners | 1 |
| Raffle default winners | `!raffle` and `!sraffle` 1, `!mraffle` 3 |
| Raffle auto winners | `ceil(entrants / 10)`, at most the maximum and the prize |
| Claim window | off, or 30 s to 15 min; default 2 min |
| Mod action timeout for stuck verification | 30 min for giveaways and raffles |
| Freeze maximum | 30 min |
| Giveaway and raffle modules, luck, skip recent winners, transfer block, close games while frozen | default off |
| Freeze standings while open | default on when the preset has a points or watch time rule, otherwise off |
| Join refusal line | default on |
| Successful join line | default off |

## 17. Tests

Through exported seams (the `engine/draw` store API, module `Process`, loyalty repositories and RPC handlers):
- `engine/draw` store on local Valkey: join after close refused; stale timer on a reopened round is a no-op; crash between close and payout resumes from the due sweep; reroll excludes drawn viewers and opens a fresh window; claim on an old generation refused; two concurrent draws produce one; `auto` never decreases as entrants rise; rejection sampling stays uniform over eligible entrants (fixed-seed statistical check).
- Loyalty repository on SQLite, plus MySQL tests for isolation: replayed key credits once; mismatched payload refused; freeze copy equals balances at `as_of`; accrual during a freeze changes `balances` and not the snapshot; the copy does not block a concurrent watch award under READ COMMITTED; expired freeze reads live; release with an old `freeze_id` is a no-op; reset while frozen refused; account deletion removes snapshot rows.
- Eligibility through the module `Process` path with fakes: one table of rules, inputs and refusal text.
- The policy pin table above as one test.

## 18. Delivery plan

One PR each; main stays shippable after every step.

1. Loyalty: `balance.credit` (keyed, idempotent), `watch.status`, `balance.snapshot` (live only). Sesame tick state `last_page_at`.
2. Outgress: bucket charging and typed retry on `followage.get` and `accountage.get`.
3. Legacy raffle `Open` gate on `draw:joinable` (ships alone, before step 4).
4. `pkg/valkey` round operations and `engine/draw` with its store tests, the shared viewer command owner and legacy routing.
5. Raffle on `engine/draw`: explicit winners, `auto`, exact payout, re-parent under Loyalty, Games tab.
6. Freeze: tables, verbs, frozen `top.get`, public banner, purge consumer, dashboard control, `!freeze`.
7. `engine/eligibility`, `!check`, join refusals.
8. Giveaway module, live panel, presets, Modules winner rows, public past winners.
9. Amount grammar and gamble id cooldown.
10. Remove the legacy `raffle:*` store after drain.

## 19. Not building

- Prize key storage or automatic key delivery.
- Points-bought entries for item giveaways.
- Chat setup wizards and entrant dumps in chat.
- Scheduled automatic loyalty resets (owner decision, 2026-10-21).
