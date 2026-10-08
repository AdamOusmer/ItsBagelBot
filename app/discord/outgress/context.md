# Discord outgress context

Agent guide for Discord REST execution/setup. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for the ingress/engine/data owners.

## Purpose and boundary

Outgress consumes Discord commands and performs Discord REST effects. It also
serves dashboard-facing setup/layout/config/unbind/post RPC and private engine
channel/live/ticket orchestration RPC. It owns slash-command registration.
There is no gateway Identify session here; multiple replicas share fleet API rate
coordination. Discord ingress separately makes only the immediate interaction
defer call against that same bot-token rate budget.
Discord-data owns MySQL guild bindings/settings/tickets/XP. Outgress reaches it
through `discordstore.NewRPC`; Valkey holds caches and ephemeral execution state.

## Local nomenclature

- **Command:** described action emitted by engine, targeting guild/channel/member.
- **Mod lane:** latency-sensitive delete/ban/kick/timeout/strip/lockdown/unlock.
- **Default lane:** welcomes/posts/embeds/followups/roles/identity and other effects.
  The split is urgency, not Premium customer tier.
- **Application ID:** learned from Discord at bootstrap, used by followup paths.
- **Setup:** create/adopt template resources and bind a guild to a broadcaster.
- **Pinned role:** selected existing guild role adopted for a template slot.
- **Desk panel:** remembered ticket entrypoint message, replaceable by repost RPC.
- **Lockdown snapshot:** previous verification/overwrite state retained for unlock.
- **Live memo:** IDs/state for updating a go-live post and role on stream end.
- **Guild identity:** the bot's per-guild nickname/avatar, not member account identity.
- **Strict ownership:** dashboard writes require authoritative data-service binding;
  a cache-only remembered relationship is insufficient authority.

## Code navigation

| Change | Read first |
| --- | --- |
| Boot, stream and RPC assembly | [main.go](main.go): `ensureOutgressStream`, `registerSlashCommands`, `subscribeRPCs`, `startCommandConsumer` |
| Configuration/default compatibility | [internal/config/config.go](internal/config/config.go) |
| Urgency scheduling and ACK/NAK | [internal/commands/consumer.go](internal/commands/consumer.go): `Run`, `pump`, `pollMod`, `process` |
| Typed REST dispatch | [internal/commands/handlers.go](internal/commands/handlers.go): `Handlers.Dispatch` |
| Raid/strip/lockdown state | [internal/commands/raid.go](internal/commands/raid.go), [internal/kv/kv.go](internal/kv/kv.go) |
| Slash-command catalog/bootstrap | [internal/bootstrap/bootstrap.go](internal/bootstrap/bootstrap.go) |
| Guild create/adopt/bind/layout | [internal/setup/setup.go](internal/setup/setup.go): `SetupGuild`, `GuildLayout`, `UnbindGuild` |
| Per-guild config/version/ownership | [internal/setup/config.go](internal/setup/config.go): `GuildConfig`, `SetGuildConfig`, `ListGuilds`, `requireOwnerStrict` |
| Ticket desk replacement | [internal/setup/desk.go](internal/setup/desk.go): `RepostDesk`; [worker.go](internal/setup/worker.go) |
| Dashboard-facing RPC wiring | [internal/rpc/setup_rpc.go](internal/rpc/setup_rpc.go), [register.go](internal/rpc/register.go) |
| Engine channel/live/purge RPC | [internal/rpc/engine_rpc.go](internal/rpc/engine_rpc.go) |
| Ticket create/close/archive RPC | [internal/rpc/ticket_rpc.go](internal/rpc/ticket_rpc.go) |
| Premium appearance assets | [internal/identity/identity.go](internal/identity/identity.go) |
| Shared effects/rate/state contracts | [Discord domain](../../../internal/domain/discord), [discordapi](../../../internal/discordapi), [discordrate](../../../internal/discordrate), [discordstore](../../../internal/discordstore) |

## Inputs, output and contracts

Outgress provisions `DISCORD_OUTGRESS` before subscribing. It drains
`discord.outgress.mod` before ordinary work on `discord.outgress.default`.
Shared [Command](../../../internal/domain/discord/command.go) carries type,
guild/channel/user target, action payload and audit reason. Moderation classification
is centralized in `discord.ModType`/`discord.Lane`; update it for any new action.
Malformed commands acknowledge/drop; handler failures NAK with a one-second paced,
three-redelivery budget. The stream retains pending commands for 60 seconds.

The mod-first loop polls moderation before waiting for either lane. A select race
can allow one default send when both become ready; it is priority scheduling,
not a linearizable guarantee that no default request can ever precede moderation.
Global rate coordination is Valkey-backed and keyed by the fleet bot token; API
client methods share it across workers, RPCs and ingress defers.

Dashboard RPC retains historical prefix `bagel.rpc.dingress` and environment names
`NATS_DINGRESS_RPC_PREFIX`/`QUEUE`. This is compatibility vocabulary, not an old
combined runtime. Private engine RPC uses `bagel.rpc.discord-outgress`.
[Historical dashboard Discord RPC](../../../internal/domain/rpc/outgress/discord.go)
and private [Discord outgress RPC](../../../internal/domain/rpc/discordoutgress)
are distinct contracts. The first still lives under the shared `rpc/outgress`
package despite describing Discord setup; do not infer Twitch-only ownership
from that directory name.

Setup reads existing resources and adopts/fills template slots, including pinned
roles. Lived-in servers get only the missing voice hub, voice category, logs
channel and ticket/archive categories (no roles, no other channels). Every
created gated or read-only channel carries an explicit bot member overwrite.
Config writes verify all configured channel ids (including up to 25 ignored
channels) against one guild channel listing. Config writes have versions and authoritative binding checks. Desk repost
remembers the new panel and handles prior deletion independently; ticket close
orchestration preserves/archive state through its typed RPC/store contract.
Live/reauth/bot-status/lockdown memos are execution projections, not settings truth.

## Invariants and pitfalls

- Never add a gateway session or independent per-pod global rate counter here.
- Engine describes community behavior; outgress executes it. Setup/ticket RPC
  orchestrations belong here when they require synchronous Discord resources.
- Empty bot token takes the explicit idle/health path; it does not register
  commands or prove real Discord connectivity.
- Slash bootstrap failure is logged and does not stop lane consumption; missing
  application ID can still impair interaction followups until a later rollout.
- Interaction followups complete already-deferred work; silent drops strand users
  in a pending response. Preserve errors and token/application targeting.
- Dashboard mutation authority cannot come solely from a cached binding. Existing
  tests explicitly require refusal when discord-data cannot establish ownership.
- Preserve restored channel overwrites/verification on unlock and the role hierarchy
  rules when stripping roles; coarse replacement can damage unrelated permissions.
- Cosmetic guild identity stays in the default lane and must not preempt moderation.
- Health checks name mod/default progress separately and feed engine's Discord
  vertical via service token `discord-outgress`.

## Local verification

Run Go commands from repository root:

```sh
go test ./app/discord/outgress/...
go test ./app/discord/outgress/internal/commands ./app/discord/outgress/internal/rpc
go test ./app/discord/outgress/internal/setup
```

Fake REST fixtures cover calls without live Discord side effects. Target ownership,
version, desk, ticket, restore and identity tests for the affected feature.
Connected `go run ./app/discord/outgress` requires NATS/Valkey/discord-data and a
development bot token; startup may register commands and pending work may execute.
