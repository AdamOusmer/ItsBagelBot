# Sesame context

Agent guide for Twitch command/event behavior. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for service ownership.

## Purpose and boundary

Sesame is the Go Twitch engine between ingress and outgress. It resolves channel
settings and custom commands, gates invocations, expands reply variables, runs
registered handlers and publishes typed Twitch requests. It does not hold Twitch
HTTP execution ownership or write authoritative module/command/user SQL rows.
Persistent owners are accessed through RPC; Projector/Valkey provide fast views.
Incoming AutoMod is implemented in this app's Go `automod/` package and engine.
Automatic enforcement defaults off (`SESAME_AUTOMOD_ENFORCE=false`). The outgoing
floor check is also in Sesame; there is no separate Outgress reply-safety layer.

## Local nomenclature

- **Module:** immutable code registration plus an optional named channel setting.
  A module's code being registered does not mean a channel enabled its setting.
- **Registry:** indexed commands/events assembled once from `modules.All`.
- **Command:** parsed `!trigger`; built-in handlers and projected custom replies
  share permission/live/cooldown gates, but have different lookup sources.
- **Regress:** ingress priority carried through execution to the outgoing lane.
- **Context:** per-invocation module state, with envelope/settings/variable scopes.
- **Output/Emit:** handler's described consequence; pipeline serializes it for
  outgress. It is not a direct Twitch API call.
- **Variable/token/form/surface/scope:** use the root reply-template glossary;
  scope is an internal resolver grouping, not broadcaster-facing terminology.
- **Projection:** derived channel state read cheaply; its owner persists truth.
- **Trial:** observed channel that exercises processing with output suppressed.
- **Sequencer:** bounded per-broadcaster FIFO for stream lifecycle followups on
  one replica (timer/loyalty arm-disarm and greet resets); cross-replica ordering
  uses versioned live writes. Queue overflow runs inline and loses FIFO ordering.

## Code navigation

| Change | Read first |
| --- | --- |
| Service wiring | [main.go](main.go), [wiring.go](wiring.go), [engine/deps.go](engine/deps.go), [internal/config/config.go](internal/config/config.go) |
| Full event flow | [engine/pipeline.go](engine/pipeline.go): `Process`, `runStages`, `newEmit`, `publishOutput`; [engine/trial.go](engine/trial.go): `processTrial` |
| Command precedence and gates | [engine/dispatch.go](engine/dispatch.go), [engine/parse.go](engine/parse.go), [engine/registry.go](engine/registry.go), [engine/module_gate.go](engine/module_gate.go) |
| Add/change a module | [modules/all.go](modules/all.go), [module/builder.go](module/builder.go), [module/module.go](module/module.go), [module/context.go](module/context.go) |
| Reply variables | [engine/vars.go](engine/vars.go), [engine/scope/token_catalog.go](engine/scope/token_catalog.go), [modules/variables.go](modules/variables.go), [module/vars.go](module/vars.go) |
| Outgoing routing and slash verbs | [engine/outgress_build.go](engine/outgress_build.go), [engine/slash.go](engine/slash.go), [shared slash grammar](../../../internal/domain/outgress/slash.go) |
| Premium voice alert output | [modules/alerts_voice.go](modules/alerts_voice.go), [voice.go](voice.go), [internal/voice/worker.go](internal/voice/worker.go) |
| Consumers and retry lanes | [internal/consumer/consumer.go](internal/consumer/consumer.go) |
| Cooldowns/dedup/counters | [engine/cooldown_valkey.go](engine/cooldown_valkey.go), [engine/cooldown_long_marker.go](engine/cooldown_long_marker.go), [engine/cooldown_left.go](engine/cooldown_left.go), [engine/dedup.go](engine/dedup.go), [engine/use_reporter.go](engine/use_reporter.go) |
| Loyalty and watch windows | [engine/loyalty_tick.go](engine/loyalty_tick.go), [engine/loyalty_schedule.go](engine/loyalty_schedule.go), [engine/loyalty_reporter.go](engine/loyalty_reporter.go), [modules/loyalty.go](modules/loyalty.go) |
| Raffles and giveaways | [engine/draw](engine/draw), [modules/draw.go](modules/draw.go), [modules/raffle.go](modules/raffle.go), [modules/giveaway.go](modules/giveaway.go) |
| Queue, duel and song queue | Corresponding `modules/` entry and `engine/*_valkey.go` store |
| External facts and lookups | `engine/*_rpc.go`, [engine/urlfetch.go](engine/urlfetch.go), [engine/gossip_rpc.go](engine/gossip_rpc.go) |
| Incoming AutoMod | [automod/gate.go](automod/gate.go), [automod/config.go](automod/config.go), [engine/moderate.go](engine/moderate.go), [modules/automod.go](modules/automod.go) |
| Manual sweep helpers | [engine/nuke.go](engine/nuke.go), [engine/recent.go](engine/recent.go), [engine/recent_valkey.go](engine/recent_valkey.go), [modules/moderation.go](modules/moderation.go) |
| Timer cadence/gates/stops | [engine/timers_valkey.go](engine/timers_valkey.go), [engine/timers_rules.go](engine/timers_rules.go), [engine/timer_vars.go](engine/timer_vars.go) |
| Live state and Twitch rechecks | [engine/live_valkey.go](engine/live_valkey.go), [engine/live_recheck.go](engine/live_recheck.go), [modules/live.go](modules/live.go) |
| Overview activity/chart observers | [activity_observer.go](activity_observer.go), [chatvolume_observer.go](chatvolume_observer.go), [engine/observe.go](engine/observe.go) |

## Inputs, state and outputs

`main` reconciles the Twitch ingress streams it consumes, preserving lane migration
order. Premium and standard lanes share an autoscaling worker pool with a Premium
reserve; receipt-flow mode adds scheduled retry lanes. Read [bus lane mode](../../../pkg/bus/lane_mode.go)
for acknowledgement behavior: `NATS_CONSUME_FLOW=on` enables receipt-flow mode;
unset/off uses explicit acknowledgements. This checkout has no v2 `chat_input`
adapter or standalone Rust moderation consumer.

`Process` decodes a pooled [lane envelope](../../../internal/domain/event/lane),
records totals, filters bot/self and invalid events, handles trial provenance,
loads projections, runs incoming moderation, dispatches an unconsumed solo-chat
command, runs event handlers and emits requests. An enforced moderation action
consumes the line; shadow verdicts record evidence while other processing continues.
Projection infrastructure failures and emit failures can return retryable errors;
individual module logic/gate failures are logged and skipped so sibling handlers
are not repeatedly fired. Poison payloads are terminal.

[Projection readers](../../../internal/projection) load module/command/user views
through Valkey and cold-owner RPC. Service cache TTL is bounded. Runtime game,
cooldown, queue and roster stores have different lifetimes from SQL-owned settings.
Timers and stream-live refreshers are wired in `main.go`; check these when behavior
occurs without a viewer command.

Outgoing contract is [outgress.Message](../../../internal/domain/outgress/message.go).
Premium/standard lane choice preserves ingress `Regress`; system jobs use the
system subject. Slash verbs are converted to typed actions before final delivery.
Variable expansion can call cached RPC readers or the Gossip custom-fetch endpoint;
a bare variable token is not necessarily a pure formatting operation.

## Premium voice alerts

Alert handlers share event decoding, token values, and follow/subscription
cooldowns. Chat and speech use separate templates and per-kind switches.
The dashboard prefills unset spoken templates in its current locale and saves
the text when the broadcaster saves voice settings. Sesame uses the built-in
English template when the stored spoken template is missing or empty.
Speech additionally checks the channel's live state, cheer threshold, and
Users allowance, which requires a configured overlay link. Viewer names and
messages are plain text, and rendered speech is stripped of control characters.
Sesame applies the channel's AutoMod blocked terms to the cheer or resub message
and drops that part on a match. The cheer event only carries a plain message,
so Sesame removes whitespace-delimited tokens made up of letters followed by
digits. Trial events never enqueue a voice job.

Sesame publishes event-ID-deduplicated jobs to the file-backed `TTS_JOBS`
work queue. The shared `tts-worker` durable allows three deliveries.
`SESAME_TTS_WORKERS` controls parallel jobs per pod and defaults to four.
Workers send progress acknowledgements, recheck Users allowance, and trim by
the clip and remaining-token budgets before calling Gossip. Rejected requests
are acknowledged, rate limits use the returned retry delay when given, and
unavailable replies retry after two seconds. The clip's
object metadata preserves duration and usage across delivery retries.
Users records usage by event ID. The overlay stream deduplicates the
playback event by the same ID. Clips and playback events expire after five
minutes in the gateway's NATS account. Sesame has a separate writer role
in that account.

`SESAME_TTS_ENABLED` turns the publisher and worker on. Keep it `false` in any
environment where the signed NATS account claims and the Sesame TTS writer
credentials are not live: startup fails without them.

## Invariants and pitfalls

- `modules.All` order is meaningful: registry command collisions are first-wins.
  Raffle owns standalone `!join`; queue remains reachable through `!queue join`.
- Add a module to the assembly list and its variables to the appropriate catalog;
  reserve existing command spellings and public namespaces.
- Pooled envelopes/contexts/outputs must not be retained after their invocation.
- `floorSuppressed` checks nonempty chat/announcement/pin output through the
  shared `internal/moderation.CheckFloor`; suppressing output is distinct from
  `moderateChat` imposing a viewer consequence. Preserve checks after expansion.
- Trial processing skips incoming moderation/refunds, exercises handlers and
  marks outputs (including batch children) with trial origin/generation before
  publication. Outgress refuses tagged output; preserve this provenance.
- `!nuke` has a moderator-gated local sweep implementation and fixtures, but
  current `buildDeps` does not wire `Deps.Nuke`; the registered command returns
  quietly when nil. `SESAME_NUKE` being parsed does not prove runtime wiring.
- Emote/lexicon refreshers and optional adaptive/link-check layers really feed
  the Go gate. Adaptive behavior defaults off; inspect main/config before edits.
- A `stream.offline` is a claim, not a fact: Twitch can dispatch a restarted
  stream's old offline after the new online. Each applied offline is rechecked
  against Helix twice while the channel stays offline, and a cold read that
  finds a projected offline rechecks at most once per window.
- Publication IDs alone do not make broker publication replay-safe; inspect current
  [bus publisher semantics](../../../pkg/bus/publish.go) before changing retries.

## Local verification

Go belongs to the repository root module; run these from the repository root:

```sh
go test ./app/twitch/sesame/...
go test ./app/twitch/sesame/engine -run 'Pipeline|Moderation|Trial|Dedup'
go test ./app/twitch/sesame/automod/...
go test ./app/twitch/sesame/modules ./app/twitch/sesame/engine/scope
```

Some Valkey suites start a disposable local server; inspect individual fixtures
before assuming a skipped integration test proves store behavior.
`go run ./app/twitch/sesame` requires NATS/Valkey/RPC dependencies from configuration.
For a reply-variable change, use catalog golden tests and the corresponding module
and scope tests; for a command change, cover gate, routing and expanded output.
