# Sesame Automod — Implementation Plan

Each phase below is a shippable PR, grounded in the current sesame code (`app/sesame/engine`, `app/sesame/module`, `app/sesame/modules`) and the live ingress squash (folded `channel.chat.message` cohorts).

## Where it plugs in

sesame's chat path today (`app/sesame/engine/pipeline.go` `Process`): decode envelope, `dispatch` a command if the line is one, `runHandlers` for the type, then `emit` through `buildOutgress`. The automod is an **inline gate that runs first on chat**, before command dispatch, with **deferred emit** (stage outputs, flush after the verdict). New package `app/sesame/automod/`. Verdict is a value type, hot path stays zero-alloc (`TestProcessNoOutputAllocCeiling` must keep passing).

## Prerequisite: the classifier artifact

Train a **fastText or GBDT** toxicity/spam model **once, offline, on public datasets** (Jigsaw, HateXplain, etc.). Ship it as a file embedded via `//go:embed`. No user data and no online training. Pure-Go inference: `leaves` for GBDT, or a fastText port. SimHash/Aho-Corasick need no artifact.

---

## Phase 0 — Wire gaps (ban/timeout/warn/delete path)

Nothing behaves differently yet (no emitter), but the moderation-action wire works end to end and is testable.

- `app/sesame/module/module.go`: `Output` += `TargetUserID string`, `Duration int` (sec; 0 = permanent ban), `Reason string`, `MsgID string`.
- `app/sesame/engine/pool.go`: zero the new fields in the `Output` pool reset.
- `app/sesame/engine/pipeline.go` `buildOutgress`: add cases `TypeTimeout` / `TypeBan` → marshal `{"data":{"user_id","duration","reason"}}` (omit duration for ban); delete-message; `TypeWarn`.
- `app/outgress/internal/worker/worker.go`: add **`processBan`** (inject `broadcaster_id` + bot `moderator_id` as query params on `/helix/moderation/bans`, mirroring `processAnnounce`). Today `TypeBan`/`TypeTimeout` route there but fall through to `processAPI` with no query params → 400. Also `TypeWarn` → `/helix/moderation/warnings`; delete → `DELETE /helix/moderation/chat?...&message_id=`.
- Preserve separate identities end to end: `Envelope.EventID` is the EventSub delivery ID used for deduplication; `Envelope.MsgID` is Twitch's chat `message_id` used for delete.
- OAuth migration before subscription/action rollout: add `moderator:manage:automod`, `moderator:manage:warnings`, `moderator:read:suspicious_users`, and `moderator:manage:shield_mode`; re-authorize the bot grant and expose missing-scope/401 failures as capability errors.
- Tests: `buildOutgress` payload marshaling, `processBan` query-param injection, alloc ceiling unchanged.

## Phase 1 — Envelope: cohort senders + fragments

- `internal/domain/event/lane/lane.go` `Envelope` += `Senders []Sender` (chatter_user_id/login, msg_id, event_id, ts, badges) and `Fragments []Fragment` (emote/cheermote/mention, from `channel.chat.message`).
- Add one shared chat-inspection iterator: a normal envelope yields its top-level sender; a folded envelope yields each duplicate sender and skips command dispatch. The first occurrence was already delivered as a normal envelope. Phase 2 must consume this iterator in shadow before enforcement can ship.
- **Ingress-side (cross-service):** subscribe to and forward `channel.suspicious_user.message`, `automod.message.hold`, `channel.shield_mode.begin/end` (add to `Ingress.Pipeline` + the Conduit subscription set). These feed Phases 3-5.

## Phase 2 — Automod gate: Tier 0 + Tier 1, SHADOW mode

New `app/sesame/automod/`:
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
- Custom-command safety: `app/sesame/engine/dispatch.go` `runCustom` → content-check the **expanded** output against the floor before emit (catches `$(query)`/`${touser}` slur injection); save-time validation in the modules service.
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
