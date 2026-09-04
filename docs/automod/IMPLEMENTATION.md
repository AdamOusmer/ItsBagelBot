<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Sesame Automod — Implementation Plan

Each phase below is a shippable PR, grounded in the current sesame code (`app/twitch/sesame/engine`, `app/twitch/sesame/module`, `app/twitch/sesame/modules`) and the live ingress squash (folded `channel.chat.message` cohorts).

## Where it plugs in

sesame's chat path today (`app/twitch/sesame/engine/pipeline.go` `Process`): decode envelope, `dispatch` a command if the line is one, `runHandlers` for the type, then `emit` through `buildOutgress`. The automod is an **inline gate that runs first on chat**, before command dispatch, with **deferred emit** (stage outputs, flush after the verdict). New package `app/twitch/sesame/automod/`. Verdict is a value type, hot path stays zero-alloc (`TestProcessNoOutputAllocCeiling` must keep passing).

## Prerequisite: the classifier artifact

Train a **fastText or GBDT** toxicity/spam model **once, offline, on public datasets** (Jigsaw, HateXplain, etc.). Ship it as a file embedded via `//go:embed`. No user data and no online training. Pure-Go inference: `leaves` for GBDT, or a fastText port. SimHash/Aho-Corasick need no artifact.

---

## Phase 0 — Wire gaps (ban/timeout/warn/delete path)

Nothing behaves differently yet (no emitter), but the moderation-action wire works end to end and is testable.

- `app/twitch/sesame/module/module.go`: `Output` += `TargetUserID string`, `Duration int` (sec; 0 = permanent ban), `Reason string`, `MsgID string`.
- `app/twitch/sesame/engine/pool.go`: zero the new fields in the `Output` pool reset.
- `app/twitch/sesame/engine/pipeline.go` `buildOutgress`: add cases `TypeTimeout` / `TypeBan` → marshal `{"data":{"user_id","duration","reason"}}` (omit duration for ban); delete-message; `TypeWarn`.
- `app/twitch/outgress/internal/worker/worker.go`: add **`processBan`** (inject `broadcaster_id` + bot `moderator_id` as query params on `/helix/moderation/bans`, mirroring `processAnnounce`). Today `TypeBan`/`TypeTimeout` route there but fall through to `processAPI` with no query params → 400. Also `TypeWarn` → `/helix/moderation/warnings`; delete → `DELETE /helix/moderation/chat?...&message_id=`.
- Preserve separate identities end to end: `Envelope.EventID` is the EventSub delivery ID used for deduplication; `Envelope.MsgID` is Twitch's chat `message_id` used for delete.
- OAuth migration before subscription/action rollout: add `moderator:manage:automod`, `moderator:manage:warnings`, `moderator:read:suspicious_users`, and `moderator:manage:shield_mode`; re-authorize the bot grant and expose missing-scope/401 failures as capability errors.
- Tests: `buildOutgress` payload marshaling, `processBan` query-param injection, alloc ceiling unchanged.

## Phase 1 — Envelope: cohort senders + fragments

- `internal/domain/event/lane/lane.go` `Envelope` += `Senders []Sender` (chatter_user_id/login, msg_id, event_id, ts, badges) and `Fragments []Fragment` (emote/cheermote/mention, from `channel.chat.message`).
- Add one shared chat-inspection iterator: a normal envelope yields its top-level sender; a folded envelope yields each duplicate sender and skips command dispatch. The first occurrence was already delivered as a normal envelope. Phase 2 must consume this iterator in shadow before enforcement can ship.
- **Ingress-side (cross-service):** subscribe to and forward `channel.suspicious_user.message`, `automod.message.hold`, `channel.shield_mode.begin/end` (add to `Ingress.Pipeline` + the Conduit subscription set). These feed Phases 3-5.

## Phase 2 — Automod gate: Tier 0 + Tier 1, SHADOW mode

New `app/twitch/sesame/automod/`:
- `verdict.go`: `Verdict{Action uint8; Seconds uint32; Rule uint8}` value type.
- `gate.go`: `Inspect(mctx) Verdict` — trust gate first (role/sub/known-chatter), then Tier 1.
- `skeleton.go`: NFKC + confusable fold + strip zero-width/RTL/Zalgo into a pooled buffer (`golang.org/x/text`). Confusable flag only on **script-mixing within a token**, never a wholesale non-latin message.
- `scan.go`: byte scanners (caps, symbol/emote ratio from `Fragments`, repeat run-length, zero-width, length) — no regex, zero-alloc.
- `match.go`: Aho-Corasick over the skeleton for slurs / scam domains / IP-logger domains; curated pattern lists.
- `classifier.go`: the embedded fastText/GBDT model → toxicity/spam score.
- `config.go`: decode per-broadcaster `automod` `ModuleView.Configs` (profile, floor, per-rule toggles); compiled-ruleset cache keyed by config hash, compiled off the hot path on change.
- Pipeline: extract shared content features once, then call `automod.Inspect` for every sender yielded by the Phase 1 iterator **before** `dispatch`; trust/reputation decisions remain sender-specific. `shadow_mode` logs verdicts + metrics and takes **no action**; deferred-emit staging lives in the pooled Context.
- Tests: skeleton, each scanner, AC match, classifier smoke, shadow verdict logging; alloc ceiling on the clean path.

Ships shadow-only → tune on real traffic before arming.

## Phase 3 — Centralized valkey brain + cohort state

- `valkey.go`: reputation `am:acct:<uid>`, campaign `am:tmpl:<simhash>`, new-chatter burst HLL `am:new:<chan>`, cohort `am:cohort:<skeleton>`, raid-mode `am:raid:<chan>`. Async, pipelined writes; TTL, no DB.
- `aggregator.go`: one background goroutine refreshes an in-process snapshot (raid-mode bitmap, flagged-account bloom, hot-template) every 250ms-1s; the gate reads only the snapshot (zero latency).
- The Phase 1 iterator updates reputation and campaign state for each folded sender. `M` distinct users on identical text is the campaign primitive, pre-assembled; it is active in shadow before any action can target users.
- Feed `suspicious_user` + `automod.hold` signals into reputation/score.
- Tests: reputation TTL, campaign corroboration, snapshot refresh, cohort fan-out.

## Phase 4 — Action decider + profiles + floor + custom-command safety

- `decider.go`: score + reputation + grace + level → `warn | delete | restrict | timeout | ban`. Ban opt-in for local rules, opt-out for confirmed network threats.
- `profiles.go`: `pg` / `moderate` / `adult` presets, plus the **immovable hate/illegal floor** (no profile or allow-term can disable it).
- Custom-command safety: `app/twitch/sesame/engine/dispatch.go` `runCustom` → content-check the **expanded** output against the floor before emit (catches `$(query)`/`${touser}` slur injection); save-time validation in the modules service.
- Emit the action via the Phase 0 wire. Grace ladder + reputation-weighted thresholds.
- Config surface: per-broadcaster profile (`pg`, `moderate`, `adult`), `shadow_mode`, per-rule toggles, allow terms, grace thresholds, and explicit local-rule ban opt-in. The hate/illegal floor and confirmed network-threat response cannot be disabled.
- Tests: decider ladder, floor immovability, expanded-command floor block, profile behavior.

Ships enforcement for opted-in broadcasters.

## Phase 5 — Mass-raid escalation + moderation budget

- outgress: Shield Mode `PUT /helix/moderation/shield_mode` + followers/sub/emote-only chat-settings; surface `Ratelimit-Remaining`/`Reset`.
- automod: budget circuit-breaker (~400/min of the 800/min cap), escalate to Shield Mode / channel-level defense when a raid exceeds budget; consume `shield_mode.begin/end`.

## Phase 6 — Deep FP layer

- `baseline.go`: per-channel adaptive baseline (valkey counters), score anomalies relative to the channel.
- Four-band adjudication: act-now / clear-allow / uncertain-lexical (classifier) / uncertain-semantic → **human mod queue**.
- Mod queue: curated (reputation-weighted, deduped, ranked), surfaced in the console (reuse staff auth + notifications bell).
- Ecosystem: per-channel BTTV/FFZ/7TV emote-code sets (fetch + cache + hot-reload) so emote spam is not word-spam; 7TV zero-width overlay whitelist; bot allowlist.
- Pattern config artifact (cohorts/templates/domains) via git + Flux hot-reload.

---

## State of the world — 2026-08 FP-hardening pass

Phases 0-2 plus the cohort/reputation/campaign core of Phases 3-5 are **built and running in shadow** (`SESAME_AUTOMOD_ENFORCE=false`, default). A ~24h shadow-mode audit on 2026-08-22 measured **precision 0/8** — every detection was a heuristic-delete false positive (`LUL LUL LUL LUL`, a bare `^`, `???`) — which motivated this pass. It ships behind the shadow switch; nothing here changes an enforced verdict until re-audited (see Rollout below).

**Gate shape (two tiers):** Tier 0 trust exempts VIP-and-up before any scan (`gate.go` `Assess`). Tier 1 runs the council on the deep path in fixed order: immovable infra floor -> language juror -> hate lexicon (immovable) -> gated lexicon categories -> channel block-terms -> heuristics with the rescues below. The clean bail stays zero-alloc and now pre-scans **both** floor halves allocation-free (`FloorPrescan` for the lexicon, `MatchFloorPrescan` for infrastructure), so a short slur/scam/host line routes deep instead of bailing clean; the pre-scans only ever route, never verdict.

**Heuristic rescues** (`heuristicVerdict`; each requires exactly ONE flag raised — multi-flag shapes never suppress):
- **Caps-only:** suppressed when the tokens are known emotes — fetched BTTV/FFZ/7TV codes **or** the static native-Twitch classics (`nativeTwitchEmotes`: twelve pre-2020 globals incl. `LUL`; they merge at *lookup* time so `EmoteSet.Len` keeps reporting what was fetched and an ops-installed empty set stays "no third-party knowledge"). When the gate holds **no emote knowledge at all** (`emotesUnavailable`: never loaded or explicitly cleared — a total refresh failure keeps the previous set, so nil is deliberate state), caps-only lines suppress toward leniency instead of deleting on an unverifiable guess; a loaded-but-empty set is an ops decision and keeps enforcing for non-emote shouting.
- **Symbol-only + emoji-dominant:** pure-emoji hype reads as symbol spam because pictographs count as symbols. When emoji carry at least half the non-space runes (`emojiDominant`, same 0.5 rule as `emoteMajority`), the line is hype, not abuse. The emoji counter is deliberately **additive** — pictographs still count in `symbols` too, because removing them from the numerator would cap symbolRatio at 0.5 whenever emoji dominate and make the >=0.6 threshold arithmetically unreachable. ZWJ (U+200D) and VS16 (U+FE0F) route into the emoji counter instead of zero-width (they are the composition glue of multi-codepoint emoji, not evasion; every other invisible still counts), and ZWJ alone mints no symbol.
- **symbolMinCount = 8:** the symbol flag needs ratio >=0.6 **and** >=8 symbol runes — the audited `^`/`???` deletes both measured ratio 1.0 on tiny lines. It gates only the style symbol flag; zero-width, repeat-run and other evasion signals stay ungated by design ("a wall, not a sentence"). Seven-symbol walls remain tolerated deliberately.

**Word-bounded floor matching** (`internal/moderation/floor.go` `MatchFloor`, replacing raw substring Contains): IP-logger hosts hit only at DNS-label boundaries (`https://grabify.link/x` caught; `notgrabify.link`, `grabify.links` released); scam bait hits only as adjacent whole tokens across any separator run (`FREE,NITRO!!` caught; `free nitrogen` clean). Known false negative, accepted: one fused token (`freenitro`) misses — splitting fused words needs edit-distance machinery whose own FP rate is not worth 600s timeouts. `FloorKind.String()` is exactly the `defaultCategories` rule name, so verdicts keep `ip_logger`/`scam`. `CheckFloor` (save-time command safety) shares the domain semantics; scam phrasing stays save-time-exempt there.

**Leet-fold quorum + ASCII fast path** (`internal/moderation/skeleton.go` `Normalize`): digits/symbols (`0 1 3 4 5 7 8 @ $`) fold only when their whitespace-token carries >=2 real letters (Cyrillic/Greek lookalike *letters* count toward the quorum; gated digits never count themselves, strippable runes are token-transparent) — `h4te` still folds, `1080`/`1337`/`<3` keep their digits. Pure-ASCII input takes a byte-wise fast path that skips NFKC and rune decoding entirely. Measured (M1 Pro): ascii 1117-1204 -> 332-346 ns/op (**~3.4x**, now 0 allocs), leet similar; non-ascii keeps its NFKC allocation and pays ~12% for the quorum lookaheads (`BenchmarkNormalize`).

**Linkish marker table widened** (`gate.go` `linkMarkers`): the campaign juror's observation-only link signal is now a package-level allocation-free table — common TLDs (`.com .net .org .gg .io .ly .tv .me .xyz .site .shop .link`), punycode `xn--`, and shortener hosts (`bit.ly t.ly cutt.ly tinyurl.com is.gd t.co`) beside `http`/`www.`. Entries subsumed by a wider substring stay listed on purpose, and two-letter TLDs beyond the listed ones are deliberately excluded (ordinary prose hits).

**Enforcement-gated reputation strikes** (`moderate.go`): the ladder lookup and strike writes ride behind `SESAME_AUTOMOD_ENFORCE` in both the single-chatter and cohort gates — shadow verdicts accrue **nothing**, so arming enforcement later cannot inherit escalated punishments from history that was never actioned. Cohort fan-out only scores a fold that is hostile AND enforced (benign copypasta folds score nobody); a mass raid's Shield Mode activation counts as its own detection event (`shield_mode` bucket).

**Tenant-scoped campaign keys** (`campaign.go`): HLLs moved from fleet-wide `am:tmpl:<band>` to `am:tmpl:<broadcasterID>:<band>` — unrelated channels posting the same meme template no longer fuse into one quorum. Old keys carry no state worth keeping; they expire through the unchanged 10-minute sliding TTL, so there is no cleanup pass. PFADD/EXPIRE write failures now surface (one debug line per 30s carrying the suppressed count) instead of vanishing while PFCOUNT still read fine; reads stay fail-open (count 0).

**Detection-flag counters + log fields** (`bot_stats.go`): every non-none verdict bumps atomics only — fleet `flags_total`, `flags_enforced`, and one of eleven named rule buckets (+`other`, closed set capped at 24 slots; escalation suffixes like `+campaign`/`+repeat` fold onto the base rule). They surface **as log fields, not loyalty counters**: `"automod detection flags"` fleet-wide every 2s with `flags_total`/`flags_enforced`/`flag_rule_*` (nonzero buckets only), and `"automod detection flags by channel"` every 30s (15 flush ticks) listing `{broadcaster_id, flags_total, flags_enforced}` per flagged channel. NewPipeline passes `d.Log` into the sink — without it both windows land in `zap.NewNop()` and the precision audit has no evidence to query.

**Pipeline alloc trims** (`pipeline.go`): the per-message ModuleView map is pooled (`GetModuleViews`/`PutModuleViews`, cleared on release) — measured 14 -> 12 allocs/op, 1585 -> 705 B/op on `BenchmarkProcessNoOutputWithViews`, guarded by a new views-path alloc ceiling alongside the original (12 non-race / 16 race; views 13 / 17). The replay base is now computed lazily on first emitted output instead of eagerly per message: alloc-neutral today but it skips sha256+hex+concat on every silent line (the raid-burst shape) and pins the floor if the base ever escapes.

**Fuzz harnesses + two fuzz-found fixes** (`internal/moderation`): `FuzzNormalize` pins lowercase/collapse/idempotency on arbitrary input and found (a) invalid UTF-8 passing a trailing incomplete sequence through unnormalized — now sanitized via `ToValidUTF8` before NFKC; and (b) `\v`/`\f` being both strippable controls and `unicode.IsSpace`, breaking token-boundary accounting and skeleton idempotence — strippable runes are now token-*transparent* before the space break. `FuzzMatchFloor` pins both floor paths plus benign shapes and found the pre-scan over-routing on control-byte glue (`0<NUL>grA81fY.l1nk`); the pre-scan is documented one-directional **both** ways vs the deep scan — routing-only, so over-routes cost one deep trip and can never mint a verdict. Differential Aho-Corasick tests pin `find`/`findFolded` against naive scans.

**Rollout path:** shadow (today) -> `SESAME_AUTOMOD_ENFORCE=true` -> `SESAME_AUTOMOD_SHIELD=true`. Audit evidence note: the 2026-08-22 sample (0/8 precision) is what motivated this FP work — **re-audit via the New Relic entity `ItsBagelBot-sesame` over >=7 days** of shadow logs (rule split from `flag_rule_*`) before arming enforcement, then again before Shield Mode.

### Known follow-ups
- Campaign juror does six valkey round-trip commands per observed line (PFADD x2, EXPIRE x2, PFCOUNT x2): coalesce the writes and move PFCOUNT onto a 1Hz ticker reading cached counts.
- Adopt `x/time/rate` for the budget circuit-breaker instead of hand-rolled counting (Phase 5).
- Subscriber exemption + command-context carve-out consideration: subs currently get Tier 0 only via roles; decide whether the trust gate should also exempt command invocations from Tier 1.
- `APP_ENV=production` JSON logging caveat: zap sampling would cap burst counts exactly during raids, when the detection-flag lines matter most — raise `Initial`/`Thereafter` or exempt verdict/flag lines from sampling before relying on them in prod.

## State of the world — 2026-08-23 learned layers + span contract

The learned layers are **wired and shadow-first by construction**: `main.go` builds one `Vocab` (`NewVocab`) and one `Baseline` (`NewBaseline(DefaultCeiling)`) per process and installs them on the shared gate via `Gate.SetExtraEmotes(vocab)` / `Gate.SetBaseline(baseline)`. Every path they touch is reduce-only — raise a threshold or shed style evidence — so installing or removing either layer can never mint a verdict that would not have existed before. Both stay inert unless the caller scopes the line with `WithChannel(ch)` (`ch != 0`), which `engine/moderate.go` does from `mctx.BroadcasterID` on both the single-chatter (`gateChat`) and cohort (`gateCohort`) paths; legacy call sites and tests see byte-identical behavior.

**Emote-span contract (producer → consumer):** ingress (`app/twitch/ingress/lib/ingress/pipeline.ex`, `emote_spans/1`) walks a `channel.chat.message` event's `message.fragments` and emits one `%{id, begin, end}` per fragment of type `emote` or `cheermote`. Offsets are **Unicode codepoint positions into `message.text`** — `begin` at the fragment's first codepoint, `end` exclusive — computed with charlist length because Twitch's own IRC-style indices are codepoints (grapheme counting drifts after flag/ZWJ emoji; byte size over-counts all non-ASCII). Cheermote fragments carry their **prefix id** (`fragment_id/1`: emote → `id`, cheermote → `prefix`; the worker reads bits/tier from the covered text itself). The spans ride `Envelope.Emotes` (`lane.EmoteSpan{ID,Begin,End}`, rune-indexed, absent when none) onto every chat envelope and identically onto a squashed cohort's base event. Consumers slice `[]rune(Env.Text)` — `module.Context.EmoteCodes()` builds the per-message lowercased code set lazily, skipping malformed/out-of-range spans rather than fataling.

**Layered emote lookup precedence** (`Gate.emoteDominant`, consulted only for caps-only flags): each whitespace token resolves against three layers in order — (a) **message spans** (`WithMessageEmotes` codes, lowercased): per-message ground truth, authoritative even when every third-party fetch failed; (b) **fetched BTTV/FFZ/7TV set**, exact-case: still required because third-party codes arrive as plain text with no spans; (c) **ExtraEmotes / learned Vocab.Known(ch, token)**, nil-safe, scoped to the line's channel. Span presence makes availability true regardless of (b)'s state; without spans the fetched layer's loaded-empty-vs-never-loaded semantics decide as before.

**Learned vocabulary mechanics** (`vocab.go`): Misra-Gries top-K per channel with d-sender promotion and hourly half-life decay. A token becomes Known only after `vocabTau=20` decaying uses from `vocabSenders=8` distinct senders (one account flooding can never mint sender diversity; coordinated slow-launder across ≥d accounts over hours is the accepted residual, mitigated by decay + purge-on-strike). Windows: `vocabBins=512` bins/channel (~180KB worst case, typical orders lower), `vocabChanCap=4096` channels/shard × `baselineShards=64` shards with stalest-half eviction — same memory discipline as bot_stats.go. Tokens lowercase at Observe AND Known, matching EmoteCodes' lowercased span codes.

**Baseline mechanics** (`baseline.go`): per-channel two-moment EWMA (`ewmaAlpha=0.05`, ~20-line memory — FP storms come in bursts, so no chasing single loud minutes) of capsRatio/symbolRatio/tokenCount observed for EVERY judged line before verdict resolution. Effective threshold = max(fleet ceiling, mean+`zScore·σ`, static config value) once warm (`coldFloorN=50` lines; below that the variance estimate is garbage and the static value passes through verbatim); `zScore=2.0` ≈ 97.7th percentile of the channel's own history. Direction is raise-only with both floors applied on cold AND warm paths (`Adjust` → `floored`), so a stricter profile survives adaptation from line one and hype channels converge toward tolerance while cold channels keep the exact static enforcement. KindTokenLen is observed only (no token-len ceiling exists to adjust).

| Constant | Value | Rationale |
|---|---|---|
| `vocabTau` | 20 | Below this, noise dominates: audit FP corpus was one-off reaction spam clearing any low bar instantly |
| `vocabSenders` | 8 | Laundering needs 8 colluding accounts per token per channel; smaller d was trivially brigaded in raid-shaped traffic |
| `vocabBins` | 512 | True heavy hitters number in the dozens; 10x headroom, bounded memory |
| `ewmaAlpha` | 0.05 | ~20-line memory; must not chase a single loud minute |
| `zScore` | 2.0 | Raise only for genuinely hotter-than-normal lines |
| `coldFloorN` | 50 | Early stats would only ever add risk of a bad raise |
| shard/cap geometry | 64 shards, 4096 chans | bot_stats.go `channelStatsMaxKeys` discipline |

**Learned-token style suppression:** when caps or symbol already flagged on the full view, `stripLearned` subtracts exactly the letters/upper/symbols that Known tokens contributed (mirroring scan()'s classifier per rune) and re-runs those comparisons. Only STYLE evidence yields — zeroWidth and repeat-run are evasion signals computed once from the full line and never suppressed; runes/spaces/emoji counts stay untouched so the emoji-hype rescue's arithmetic cannot be corrupted. The recompute runs only after a flag fired, so it can drop a flag but never mint one.

**Purge-on-strike:** any lexicon/hate-floor/block-term verdict calls `Vocab.PurgeTokens(ch, messageTokens)` — the struck message's tokens lose their counts AND sender sets, so "get it learned, then attach slurs" resets to zero on first enforcement contact and a purged token re-flags immediately. Infra-floor hits (ip_logger/scam) deliberately do not purge; scope is the struck channel only.

**Hardcoded-free status:** nothing in the gate freezes emote names anymore — the static native-Twitch list is gone (deleted 2026-08-23, user mandate), spans cover native emotes/cheermotes, the fetched set covers third-party codes, and learned Vocab covers communal channel slang. The only name tables left are the link-marker substrings (campaign juror, recall-heavy by design) and the curated lexicon/floor artifacts, which ops extend via mounted files, not rebuilds.

**Ops follow-up (unbuilt):** enabling `channel.suspicious_user.message` and `automod.message.hold` v2 needs bot-account OAuth grants `moderator:read:suspicious_users` and `moderator:manage:automod` (re-authorize the bot grant; expose missing-scope/401s as capability errors), plus appending the two specs to `ChannelOptionalSubscriptions` in `app/twitch/outgress/internal/twitch/eventsub.go` — subscriptions are created by outgress, not ingress, and both belong in the OPTIONAL list (create failure must not fail enroll; channels lacking the grants answer 403/401 permanently until re-consent).

---

## Cross-cutting

- **Zero-alloc:** every phase keeps the clean path allocation-free; skeleton/classifier use pools; the deep path runs only on flagged messages.
- **No data collection:** classifier pretrained on public data, local inference, Valkey state is ephemeral and TTL-bound.
- **Observability:** shadow-mode verdict logs, action counters, mod-override feedback (drives FP review; not a training loop).
- **Scale:** load-test all-chat ingress volume and deploy KEDA on sesame (lag-based on NATS `num_pending`) as part of the production rollout, not merely before enforcement. The premium-reserve ceil fix is already shipped.

## Order / dependencies

- Phase 0 (wire, OAuth scopes, bot re-authorization) blocks Phases 4 and 5 (actions).
- Phase 1 (envelope, sender iterator, ingress subscriptions) blocks Phases 2-3.
- Phase 2 runs every sender in shadow; Phase 3 makes cohort/reputation state real; only then may Phase 4 enforce. Continue 4 → 5 → 6.
- Prerequisite (classifier artifact) blocks Phase 2's `classifier.go` only; the rest of Phase 2 (skeleton, scanners, AC) does not need it.
