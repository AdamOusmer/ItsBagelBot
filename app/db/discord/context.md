# Discord data service

Agent guide for `app/db/discord` (runtime name `discord-data`). See the
[shared vocabulary](../../../CONTEXT.md) and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns durable guild-to-broadcaster bindings, per-guild settings, member XP, ticket
lifecycle rows and transcripts in `bagel_discord`. This is an RPC-only data
service: no gateway connection, Discord REST calls or JetStream event consumers.
Discord Engine and Outgress use [internal/discordstore](../../../internal/discordstore/)
to reach it; their Valkey caches and cooldowns remain outside this service.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| Guild | Discord server; its ID is an opaque snowflake **string**. |
| Broadcaster | Twitch owner, addressed by numeric `uint64` ID. |
| Binding / `GuildBinding` | Authoritative guild → Twitch broadcaster association. |
| `installed_by` | Discord user who performed setup; audit metadata. |
| `GuildConfig` | One guild's full `internal/domain/discord.Config` JSON settings. |
| Version | Optimistic-concurrency token for saving a complete guild config. |
| Member / `MemberKey` | `(guild_id, user_id)`; `user_id` is Discord here. |
| XP / `MemberXP` | Durable accumulated experience and stored level for that member. |
| Daily | Bonus claim gated by durable `last_daily` within a transaction. |
| Ticket / `TicketKey` | Private support-channel lifecycle keyed by guild/channel. |
| Live ticket | `open` or `claimed`, counted against the opener's limit. |
| Transcript | Plain-text history stored with its ticket, not an object-store URL. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Driver → migration → RPC → health wiring; RPC endpoint resolution. |
| [repository/repository.go](repository/repository.go) | Store, domain errors, dialect-dependent row locks and transaction helper. |
| [repository/binding.go](repository/binding.go) | Bind, guarded unbind, lookup and multi-guild broadcaster listing. |
| [repository/config.go](repository/config.go) | Ownership verification and conditional versioned settings writes. |
| [repository/xp.go](repository/xp.go) | XP addition, level derivation, daily cooldown and leaderboard. |
| [repository/ticket.go](repository/ticket.go) | Open/claim/close transitions, live counts and cursor paging. |
| [repository/transcript.go](repository/transcript.go) | Transcript persistence/readback. |
| [rpc/rpc.go](rpc/rpc.go) | Consumer-side interfaces, wiring, common request budget and error mapping. |
| [rpc/binding.go](rpc/binding.go), [config.go](rpc/config.go), [xp.go](rpc/xp.go), [ticket.go](rpc/ticket.go) | Wire validation and response views for each feature. |
| [ent/schema/](ent/schema/) | Five entities and their durable uniqueness/index constraints. |

Contracts are in [internal/domain/rpc/discorddata](../../../internal/domain/rpc/discorddata/);
guild config and leveling vocabulary are in
[internal/domain/discord](../../../internal/domain/discord/).
Edit schemas rather than generated Ent clients.

## Flow and contracts

- Default RPC prefix: `bagel.rpc.discord-data`, queue `discord-data-rpc`.
  Binding, config, ticket, transcript and XP verbs share a 3-second handler budget.
- Engine/Outgress ask the RPC adapter to read/write durable state, then handle
  Discord actions or their own cache maintenance.
- Per-guild settings moved out of the broadcaster-keyed Discord module blob.
  The Modules service retains the channel master switch; this service owns guild
  channels, roles and feature settings.
- Health checks the SQL pool and RPC connection. No BUS identity or event lane
  should be introduced by substituting `svcboot.MustNATS` for the RPC-only boot.

## Invariants and pitfalls

- One guild has one broadcaster; one broadcaster can own many guilds. Only
  `guild_id` is unique. Rebinding to a different owner requires explicit unbind.
- A guarded unbind checks the current broadcaster so a delayed request cannot
  remove a new owner's binding. Missing bindings are normal successful lookups.
- Config writes verify binding ownership before version details. Expected version
  zero means no existing row; first save becomes 1. Stale or racing saves conflict.
  Keep the conditional `UPDATE ... WHERE version = expected`, not read-then-write.
- XP totals and levels are durable; per-message earning cooldown remains Valkey
  state elsewhere. Derive levels with the existing domain curve on writes.
- Ticket channel ID is globally unique and history outlives its Discord channel.
  Transitions use guild+channel identity to avoid cross-guild access.
- The ticket-open limit is **not** absolute under MySQL READ-COMMITTED: two first
  opens can race when there are no existing rows to lock. `ticket.go` documents it.
- Transcript RPC caps body size at 2 MiB. The same-schema Ent edge cascades the
  transcript when its ticket is deleted. SQLite tests do not exercise MySQL locks.
- Keep `ent/runtime` in boot so field defaults/hooks initialize.

## Focused checks

From repository root:

```sh
go test ./app/db/discord ./app/db/discord/repository ./app/db/discord/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/discord/ent/schema
```

Choose `binding_test.go`, `config_test.go`, `xp_test.go`, `ticket_test.go` for
repository behavior; `rpc/config_test.go` pins snowflake validation.
CI's Ent generation and race run are in
[main.yml](../../../.github/workflows/main.yml).
