# Discord engine context

Agent guide for Discord community behavior. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for neighboring owners.

## Purpose and boundary

Engine turns Discord gateway events, slash/button interactions and Twitch facts
into described Discord REST commands or synchronous outgress orchestration RPCs.
It has no gateway Identify session and no direct Discord REST client.
Ingress receives/defers; outgress executes. Discord-data owns durable guild
bindings/settings, tickets and XP; Valkey supplies caches and ephemeral stores.
Projector provides broadcaster module/tier views, not per-guild settings truth.

## Local nomenclature

- **Guild binding:** a Discord guild's relationship to a Twitch broadcaster;
  a broadcaster can own multiple guild bindings.
- **Discord module:** broadcaster-level master switch, gated for the Premium beta.
- **Guild config:** one guild's feature flags/copy/channel/role settings from
  discord-data; not the old single broadcaster module blob.
- **Registry/module:** immutable handlers indexed by gateway type, slash name or
  exact button custom ID; it resembles Sesame's authoring shape but differs in axes.
- **Context:** incoming event plus resolved config, broadcaster ID and logger.
- **Command/Emit:** described Discord effect for outgress; no local REST call.
- **Ticket desk:** remembered panel/channel used to open support tickets.
- **Join-to-create:** managed voice-room lifecycle triggered by voice events.
- **LinkGuard:** cross-channel/cross-guild link-spread detection with deletions,
  distinct from Twitch's Sesame AutoMod gate.
- **Identity:** applied per-guild bot appearance associated with account tier.
- **Log category:** a toggleable class of audit log (messages, members, moderation,
  channels, roles, server, voice) with its own enable flag; each may route to its
  own **category channel**, falling back to the default log channel.
- **Ignore list / ignore bots:** channels whose events are never logged, and a
  switch that drops bot-authored events from message and member logs.
- **Message cache:** 1h copy of a message (author, content, attachments, bot flag)
  written by Message on MESSAGE_CREATE only while message logs are on, refreshed by
  Logs on edit; delete and edit logs read it. **Member roles and labels** (nick,
  channel, role and guild names) are cached 24h to compute before/after diffs.
- **Voice occupancy:** one atomic Lua transition per voice-state event returning a
  `VoiceMove` (from, to, left-empty); it fails closed (empty move) when the store errors.
- **Temp voice room:** a join-to-create clone honouring the configured category,
  name template, user limit and privacy; capped per guild atomically at track time.
- **Live/clip fact:** Twitch-derived input, not a Discord gateway dispatch.

## Code navigation

| Change | Read first |
| --- | --- |
| Wiring, consumers and confirmed publication | [main.go](main.go): `confirmedPublisher`, `startIngressConsumers`, `startTwitchConsumers`, `verticalChecks` |
| Dispatch and failure rules | [internal/dispatch/dispatch.go](internal/dispatch/dispatch.go): `Dedup`, `Handle`, `handlersFor`, `publishRetry` |
| Guild/broadcaster lookup and beta gate | [internal/resolve/resolve.go](internal/resolve/resolve.go): `ByGuild`, `ByBroadcaster`, `configOf`, `gateClosed` |
| Register handlers | [modules/all.go](modules/all.go), [module/module.go](module/module.go), [module/builder.go](module/builder.go), [internal/registry/registry.go](internal/registry/registry.go) |
| Raw gateway payload slices/permissions | [internal/decode/decode.go](internal/decode/decode.go), [internal/cmd/cmd.go](internal/cmd/cmd.go) |
| Welcome, messages and rank | [modules/welcome.go](modules/welcome.go), [message.go](modules/message.go), [rank.go](modules/rank.go) |
| Slash moderation and staff checks | [modules/moderation.go](modules/moderation.go), [modules/staff_test.go](modules/staff_test.go) |
| Ticket panel/lifecycle | [modules/ticket.go](modules/ticket.go), [shared ticket data](../../../internal/discordstore) |
| Voice rooms | [modules/voice.go](modules/voice.go), [internal/rpcclient/rpcclient.go](internal/rpcclient/rpcclient.go), [voice store](../../../internal/discordstore/store_voice.go) |
| Audit logs and their caches | [modules/logs.go](modules/logs.go) (`logTo`, `logEvent`), [message.go](modules/message.go) (cache writes), [message store](../../../internal/discordstore/store_message.go) |
| Link spread/invite exemption | [modules/linkguard.go](modules/linkguard.go), [linkguard_invite.go](modules/linkguard_invite.go), [internal/invitecache/invitecache.go](internal/invitecache/invitecache.go), [shared guard](../../../internal/domain/discord/linkguard) |
| Twitch live/clip posts | [modules/live.go](modules/live.go), [clip.go](modules/clip.go), [internal/streaminfo/streaminfo.go](internal/streaminfo/streaminfo.go) |
| Tier appearance | [modules/identity.go](modules/identity.go), [internal/identitystore/identitystore.go](internal/identitystore/identitystore.go) |
| Configuration and wire | [internal/config/config.go](internal/config/config.go), [Discord domain](../../../internal/domain/discord), [outgress RPC](../../../internal/domain/rpc/discordoutgress) |

## Inputs, state and outputs

Engine provisions `DISCORD_INGRESS` and binds the six explicit event subjects.
`Dispatcher.Handle` is wrapped by `dispatch.Dedup`: the `msg.UUID` is claimed in a
tiered LRU+Valkey store (`discord:ev:`, 2 min TTL) before anything runs, duplicates
are acked unhandled, and a store error fails open with a warning and a metric.
It then decodes `Event`, resolves guild binding/master switch/Premium
access (`gateClosed` returns a drop reason, logged once per guild and reason)
and per-guild config, ensures desk behavior, runs interested handlers and
publishes collected commands. DMs/unbound/disabled/unknown-tier inputs resolve to
no work. Config is sanitized on read; authoritative binding stamps `GuildID`.
`ByBroadcaster` fans Twitch facts out to every connected, gated guild.

Gateway events fan out to multiple interested handlers. Interactions route to
one exact slash/button owner; there is no Sesame-style `!command` parser,
role ladder, shared cooldown or live-only command gate. Discord permission and
configured staff-role checks remain feature-specific requirements.

[discordstore.NewRPC](../../../internal/discordstore) reaches discord-data for SQL
truth; ephemeral voice/desk/link counters and caches are Valkey-backed.
Channel creation, ticket orchestration and purge operations use the private
`bagel.rpc.discord-outgress` RPC rather than direct REST from this process.
Twitch live input is `twitch.ingress.event.stream`; clips use
`data.twitch.clip.created`; `data.users.changed` drives tier identity updates.
Live/Clip/identity fact consumers are wired separately from `modules.All`.

Commands use [discord.Command](../../../internal/domain/discord/command.go) and
`discord.Lane(type)` selects urgency: `discord.outgress.mod` or `.default`.
Publication uses the broker-confirmed bus path so retry classification is real.
`Dispatcher.Handle` always acknowledges its ingress event: malformed input,
resolution/handler errors and failed publication are logged or dropped, not
replayed across siblings that may already have completed side effects.

## Invariants and pitfalls

- Premium access gates the current beta centrally in resolver. Missing tier
  reader fails closed; do not duplicate/inconsistently bypass it per module.
- One broadcaster can bind many guilds; never read per-guild config from the old
  broadcaster-wide module blob or assume one guild on a Twitch live event.
- Handler error isolation is intentional; all-event replay can duplicate another
  module's successful effects or store updates.
- Confirmed publication retries only definite no-responders failures, with a
  bounded budget. A lost PubAck/closed connection can mean already-stored work;
  current fleet publishing has no broker dedup key to make retries harmless.
- Ingress already deferred interactions; always preserve token/application data
  needed for the outgress followup and log malformed interaction failures.
- LinkGuard ignores bot messages; dependency errors fail open. Observe each
  normalized link once, and emit at most one delete per message.
- Own-guild invite exemption is checked after a trip, through bounded cached
  invite resolution; ordinary messages must not pay one REST lookup per link.
- Service health answers for the whole Discord vertical, probing ingress and
  outgress and checking both Discord and Twitch consumer progress.

## Local verification

Run from repository root:

```sh
go test ./app/discord/engine/...
go test ./app/discord/engine/internal/dispatch ./app/discord/engine/internal/resolve
go test ./app/discord/engine/modules
```

Prefer the affected module fixture tests for ticket/staff/invite/config behavior.
Shared stores/LinkGuard also have tests outside this app directory. Connected
`go run ./app/discord/engine` needs NATS, Valkey, Projector, discord-data and
outgress RPCs; missing dependencies do not exercise the resolved behavior path.
