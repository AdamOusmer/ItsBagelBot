# Timers: gates and stops

Date: 2026-09-21
Status: Draft for review. Issue [#109](https://github.com/AdamOusmer/ItsBagelBot/issues/109).

## 1. Outcome

A **timer** (a broadcaster's repeating chat line, stream-only, fired off Valkey key expiry by `ValkeyTimerStore`) keeps its message and interval. Each timer gains two optional groups of settings:

- **Gates** ("only when …"): conditions checked at every tick. A tick whose gate fails **skips** the post and re-arms at the normal interval, so the timer keeps its cadence and fires on the next tick that passes. v1 ships one gate: **chat activity**, "only when at least N chat lines arrived since this timer last posted".
- **Stops** ("until …"): conditions that end the timer. A timer that has stopped does not post and does not re-arm. v1 ships two stops: a **fire cap** ("at most N posts per stream") and an **end date** ("until this date and time").

Existing timers have no gate and no stop and behave exactly as today. Nothing changes for a broadcaster who never opens the new fields.

## 2. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | Vocabulary: **timer** (the repeating line), **tick** (one key expiry), **gate** (a per-tick condition; failing it skips one post), **stop** (an end condition; reaching it ends the timer), **chat activity gate**, **fire cap**, **end date**. A **skipped** tick is not a **fire**. | Gate and stop are the two distinct behaviours in the issue's "only when" and "until"; naming them separately keeps the engine, the dashboard copy and [CONTEXT.md](../../CONTEXT.md) aligned. |
| D2 | v1 gate set: chat activity only (`minChatLines`, 0 = off, 1 to 100). Not in v1: time-of-day window, category or game, viewer count, day of week. | Chat activity is the one gate every major Twitch bot offers, and the one that solves the real complaint (a bot talking to an empty chat). The others each need data sesame does not hold today (see §11). |
| D3 | Chat lines are counted per broadcaster in Valkey (`timerx:lines:<bid>`), incremented from sesame's chat path, and only for broadcasters who have at least one enabled timer with a chat activity gate. The bot's own lines are not counted: the hook sits after the pipeline's `eligible` check, which already drops the bot's own chat by `BotUserID`. | One INCR per chat message is cheap, but not free at fleet scale; gating the increment on "someone is listening" keeps channels without a gated timer at zero extra Valkey traffic. Counting the bot's own posts would let a timer feed its own gate. |
| D4 | Each gated timer keeps a **watermark** (`timerx:mark:<bid>:<tid>`, the line counter's value at its last fire). At a tick, `counter - watermark >= minChatLines` passes. A skipped tick leaves the watermark alone, so activity accumulates across skips. A fire sets the watermark to the current counter. Both keys are deleted on `DisarmAll` (stream.offline) and carry a 48 h TTL as a safety net. | A watermark is one integer per timer and needs no reset traffic on skip; comparing against a shared counter means two gated timers with different thresholds read the same source. The TTL covers a lost stream.offline. |
| D5 | Fire cap: `maxFiresPerStream` (0 = unlimited, 1 to 100). Fires this stream are counted in `timerx:fires:<bid>:<tid>` (INCR on fire, deleted on `DisarmAll`, 48 h TTL). When the count reaches the cap, the tick does not re-arm. `ArmAll` (stream.online, mid-stream rearm, reconciler) skips a timer whose count already reached the cap. | The reconciler re-arms every live broadcaster once a minute with NX; without the `ArmAll` check it would resurrect a capped timer one minute after it stopped. The counter resets with the stream because the cap is per stream. |
| D6 | End date: `endsAt`, an RFC 3339 UTC instant, empty = never. A tick at or past `endsAt` neither fires nor re-arms; `ArmAll` skips it. The timer record stays as is (`enabled` untouched); the dashboard shows an **Ended** chip and the row stays editable. | sesame reads the timers blob and never writes it; flipping `enabled` from the engine would add a write path for one cosmetic change. A visible chip and an editable date are enough. |
| D7 | The end date is picked in the dashboard with a `datetime-local` input in the browser's zone, converted to a UTC instant on save, with the zone abbreviation shown next to the field. It does not depend on the Local Time module's home zone. | The broadcaster editing the timer is in front of the browser; using its zone needs no cross-module dependency and no "set your timezone first" dead end. |
| D8 | A gate-skipped tick re-arms at the exact interval (no jitter). Jitter stays on the first arm only, as today. | Cadence is the contract the broadcaster set; a skip is not a reason to drift it. |
| D9 | The fire cap counts fires only; a skipped tick does not consume the cap. A timer with both a gate and a cap posts at most `cap` times, each one on a tick that passed the gate. | "At most 3 announcements per stream, and only when chat is active" is the composition a broadcaster expects. |
| D10 | Timer messages stay raw text. A timer that posts a custom command's response is out of scope. | `fire` posts with no scope chain; running a command from a tick means building a synthetic context (no chatter, no args) and is its own feature. |
| D11 | New blob fields are optional; a missing field means off. Old blobs decode unchanged; the dashboard writes all three fields on every save. | Backward compatible with every timer saved so far, and the engine never has to special-case a missing key. |
| D12 | Console validation in `+page.server.ts`: clamp `minChatLines` and `maxFiresPerStream` to their ranges (default 0), parse `endsAt` and drop it to empty when unparsable. A past end date is accepted (the timer simply shows Ended). sesame re-checks the ranges defensively at arm time, same as it floors the interval today. | Same shape as the existing interval clamp; no new validation mechanism. |
| D13 | Counting hook lives in the timers engine (`timers_valkey.go` family), exposed through a narrow `ChatLineCounter` interface (`CountChatLine(ctx, bid)`) on `Deps`, called from `Pipeline.Process` after `eligible`. The "does this broadcaster have a gated timer" answer is a `cache.Keyed[uint64,bool]` memo on the store (the same in-process cache `ValkeyLiveStore` uses), invalidated inside the existing rearm-watcher callback, which already subscribes to the modules cache-invalidation subject. | The pipeline must not decode the timers blob per message; the store already has the invalidation subscription and the config decoder, and `pkg/cache.Keyed` is the repo's one memo primitive. No second NATS subscription. |
| D15 | Valkey counters (`INCR` + `EXPIRE` in one pipelined round trip, and a read that treats a missing key as zero) go through a small shared helper in `pkg/valkey` (`Counter`), not another inline `DoMulti`. Only the timers store adopts it in this change; the older inline copies in `reputation_valkey.go` and friends stay as they are. | Every engine file hand-rolls the same two commands today; the third copy is the one that becomes a helper. Refactoring the existing copies is a separate cleanup. |
| D16 | Gate and stop decisions are pure functions over `(timerDef, counts, now)` in their own file (`timers_rules.go`), unit-tested without Valkey. The store reads the counters, calls the rules, acts on the answer. `now` is a `func() time.Time` field on the store defaulting to `time.Now`, the first clock injection in the engine package. | Keeps the tick path readable (read, decide, act) and lets the end-date and cap logic be table-tested without `VALKEY_TEST_ADDR`. |
| D14 | This spec lives at `docs/specs/timer-conditions.md`; the glossary terms land in [CONTEXT.md](../../CONTEXT.md). No ADR. | Every choice here is reversible and recorded in code comments next to the constants. |

## 3. Behaviour

| Timer settings | Tick state | Result |
| --- | --- | --- |
| no gate, no stop | live | Fires, re-arms. Unchanged from today. |
| `minChatLines: 5` | 2 lines since last fire | Skips, re-arms at interval, watermark untouched. |
| `minChatLines: 5` | 7 lines since last fire | Fires, watermark = counter, re-arms. |
| `minChatLines: 5` | first tick of the stream, 5+ lines since arm | Fires (watermark starts at the counter value at arm time). |
| `maxFiresPerStream: 3` | 3rd fire | Fires, does not re-arm. |
| `maxFiresPerStream: 3` | reconciler sweep after 3 fires | `ArmAll` skips it; stays stopped until next stream.online. |
| `maxFiresPerStream: 3` | next stream.online | Counter gone with `DisarmAll`; arms fresh, cap resets. |
| `endsAt` in the past | any | No fire, no re-arm, `ArmAll` skips. Dashboard pill: Ended. |
| `endsAt` in 10 min, interval 15 min | tick after `endsAt` | No fire. The timer does not get a "last post" on the way out. |
| gate + cap | tick fails gate | Skips; cap count unchanged. |
| any | module disabled or timer disabled | Dropped, as today. |
| any | stream.offline | `DisarmAll` deletes the timer key, watermark, fire count and the broadcaster's line counter. |

## 4. Data model

`TimerDef` ([web/kit/lib/timers.ts](../../web/kit/lib/timers.ts)) and `timerDef` ([timers_valkey.go](../../app/twitch/sesame/engine/timers_valkey.go)) gain:

| Field | JSON key | Type | Default | Range |
| --- | --- | --- | --- | --- |
| Chat activity gate | `minChatLines` | int | 0 (off) | 0 to 100 |
| Fire cap | `maxFiresPerStream` | int | 0 (unlimited) | 0 to 100 |
| End date | `endsAt` | string, RFC 3339 UTC | `""` (never) | any instant |

Stored in the existing `timers` module blob; no schema migration, no new service.

## 5. Valkey keys (sesame)

| Key | Set by | Cleared by | TTL |
| --- | --- | --- | --- |
| `timerx:lines:<bid>` | chat path INCR (D3) | `DisarmAll` | 48 h |
| `timerx:mark:<bid>:<tid>` | arm (initial) and fire (D4) | `DisarmAll` | 48 h |
| `timerx:fires:<bid>:<tid>` | fire INCR (D5) | `DisarmAll` | 48 h |

The `timerx:` prefix is deliberate: the clock keys live under `timer:` (`timerKeyPrefix`) and the expiry watcher routes every expired key with that prefix into `onExpired`. Aux keys under `timer:` would only be rejected there by a failed id parse; a distinct prefix drops them at the `HasPrefix` check and keeps their TTL expiries out of the timer path entirely.

## 6. Engine changes (`app/twitch/sesame/engine`)

`onExpired`, after the existing live and config checks:

1. Stop check: `endsAt` reached, or fire count at cap. Return without fire or re-arm.
2. Gate check: watermark delta below `minChatLines`. Re-arm at exact interval, return.
3. Fire, INCR fire count, set watermark, re-arm.

`ArmAll`/`armJittered`: skip timers whose stop already holds; on a fresh arm of a gated timer set the watermark to the current counter (NX, so a mid-stream rearm does not reset an existing watermark).

`DisarmAll`: also delete the three new keys.

New `CountChatLine(ctx, bid)` called from the chat path in `pipeline.go`, guarded by the memoised "has gated timer" answer (D13), placed after `eligible` so the bot's own lines never reach it.

## 7. Dashboard (`web/dashboard`)

[TimerEditor.svelte](../../web/dashboard/src/lib/components/timers/TimerEditor.svelte): three fields under the interval, always rendered (no conditional height): **Only when chat is active** (number, lines), **Stop after** (number, posts per stream), **Until** (`datetime-local`, with the browser zone abbreviation). Each with a one-line hint. 0 or empty means off.

[TimerRow.svelte](../../web/dashboard/src/lib/components/timers/TimerRow.svelte): status pills after the existing state pill, using the row's own `bb-tag` span idiom (not the interactive `Chip` component, which CONTEXT.md reserves for token-insert buttons): `≥ 5 lines`, `3 / stream`, `until 30 Sep`, and `Ended` when `endsAt` has passed.

[+page.server.ts](../../web/dashboard/src/routes/(app)/timers/+page.server.ts): `parseTimer` moves to `web/dashboard/src/lib/server/timers-parse.ts` (a `+page.server.ts` cannot export a helper for tests) and clamps per D12; `endsAt` parses the way `quotes/+page.server.ts` validates its date. `blankTimer()` returns the three defaults.

## 8. i18n

`web/kit/lib/i18n/locales/{en,fr}.json` under `timers`: `fieldMinChatLines`, `fieldMinChatLinesHint`, `fieldMaxFires`, `fieldMaxFiresHint`, `fieldEndsAt`, `fieldEndsAtHint`, `pillMinLines`, `pillMaxFires`, `pillUntil`, `pillEnded` (alongside the existing `chipInterval`, which keeps its name). The module description in `modules.catalog.timers.description` gains one sentence about gates and stops. Keys regenerate into `keys.d.ts`.

## 9. Tests

- `timers_valkey_test.go`: gate skip re-arms without firing; gate pass fires and moves the watermark; cap reached stops re-arm; `ArmAll` skips capped and ended timers; `DisarmAll` clears all keys; old blob without the new fields behaves as before.
- Pipeline: `CountChatLine` is not called for a broadcaster without a gated timer; bot's own line not counted.
- Console: `timers-parse.test.ts` clamp table for the three fields; unparsable `endsAt` saved as empty.
- Rules: `timers_rules_test.go` tables for gate pass/skip and stop by cap and by end date; Valkey lifecycle tests use `newHotPathTestClient` (skip without `VALKEY_TEST_ADDR`).

## 10. PR split (stacked, bottom-up)

1. `feat/sesame-timer-conditions`: this spec and the CONTEXT.md terms, `pkg/valkey` counter helper, `timerDef` fields, `timers_rules.go`, stop and gate checks in `onExpired` and `ArmAll`, key cleanup in `DisarmAll`, `ChatLineCounter` wiring into the pipeline, tests. Go only, additive, deployable alone (fields absent until the console ships).
2. `feat/console-timer-conditions` (on 1): `TimerDef` fields, editor, row pills, server clamps, EN/FR copy.

Each clears CodeScene on its own.

## 11. Out of scope

- Time-of-day window gate: needs a zone choice (home zone or browser) and overnight-wrap rules; candidate for v2 once the Local Time home zone is set on enough channels.
- Category, game, or viewer-count gates: sesame does not ingest `channel.update` or stream info today.
- Posting a custom command's response from a timer (D10).
- A lifetime fire cap across streams.
- Auto-disabling an ended timer (D6).

## 12. Open questions

| # | Question | Default if unanswered |
| --- | --- | --- |
| Q1 | Is chat activity the right sole gate for v1, or should the time-of-day window ship with it? | Chat activity only (D2). |
| Q2 | Upper bounds: 100 lines, 100 fires per stream. | Keep 100 / 100. |
| Q3 | Should a gate-skipped tick be logged at debug for support, or stay silent? | Silent; the fire path already logs failures only. |
| Q4 | Should the first tick after arm require `minChatLines` lines since arm, or since stream start? | Since arm (D4, watermark set at arm). |
