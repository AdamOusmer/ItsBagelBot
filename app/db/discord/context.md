# Discord data service

Agent guide for `app/db/discord` (runtime name `discord-data`). See the
[shared vocabulary](../../../CONTEXT.md) and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns durable guild-to-broadcaster bindings, per-guild settings, member XP, ticket
lifecycle rows and transcripts in `bagel_discord`. This is an RPC data service
with one JetStream consumer, `data.users.deleted`; it has no gateway connection
or Discord REST calls.
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
| `GuildModule` | One `(guild_id, name)` row: `enabled`, JSON `settings`, own `version`. Module names and kinds live in `internal/domain/discord` (`Modules`). Source of truth for settings. |
| Version | Optimistic-concurrency token: per `GuildConfig` row (legacy) and per `GuildModule` row. |
| Member / `MemberKey` | `(guild_id, user_id)`; `user_id` is Discord here. |
| XP / `MemberXP` | Durable accumulated experience and stored level for that member. |
| Daily | Bonus claim gated by durable `last_daily` within a transaction. |
| Ticket / `TicketKey` | Private support-channel lifecycle keyed by guild/channel. |
| Live ticket | `open` or `claimed`, counted against the opener's limit. |
| Transcript | Plain-text history stored with its ticket, not an object-store URL. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Driver → migration → deletion consumer → RPC → health wiring; RPC endpoint resolution. |
| [repository/repository.go](repository/repository.go) | Store, domain errors, dialect-dependent row locks and transaction helper. |
| [repository/binding.go](repository/binding.go) | Bind, guarded unbind, lookup and multi-guild broadcaster listing. |
| [repository/config.go](repository/config.go) | Legacy whole-guild config: ownership verification and conditional versioned writes. |
| [repository/module.go](repository/module.go) | Per-module rows: lazy backfill from legacy, versioned writes, legacy dual-write. |
| [repository/xp.go](repository/xp.go) | XP addition, level derivation, daily cooldown and leaderboard. |
| [repository/ticket.go](repository/ticket.go) | Open/claim/close transitions, live counts and cursor paging. |
| [repository/transcript.go](repository/transcript.go) | Transcript persistence/readback. |
| [repository/erase.go](repository/erase.go) | Account-deletion erasure of a broadcaster's bindings and configs. |
| [rpc/rpc.go](rpc/rpc.go) | Consumer-side interfaces, wiring, common request budget and error mapping. |
| [rpc/binding.go](rpc/binding.go), [config.go](rpc/config.go), [module.go](rpc/module.go), [xp.go](rpc/xp.go), [ticket.go](rpc/ticket.go) | Wire validation and response views for each feature. |
| [ent/schema/](ent/schema/) | Six entities and their durable uniqueness/index constraints. |

Contracts are in [internal/domain/rpc/discorddata](../../../internal/domain/rpc/discorddata/);
guild config and leveling vocabulary are in
[internal/domain/discord](../../../internal/domain/discord/).
Edit schemas rather than generated Ent clients.

## Flow and contracts

- Default RPC prefix: `bagel.rpc.discord-data`, queue `discord-data-rpc`.
  Binding, config, module, ticket, transcript and XP verbs share a 3-second handler budget.
- `module.list` / `module.set` are the live settings verbs; `config.get` / `config.set`
  stay for one release for old clients. `module.set` validates with
  `ddiscord.ValidateModule` (bad ids in `fields`), refuses unknown names and `identity`,
  forces core modules enabled, and uses `expected_version` 0 = row must not exist.
- Engine/Outgress ask the RPC adapter to read/write durable state, then handle
  Discord actions or their own cache maintenance.
- Per-guild settings moved out of the broadcaster-keyed Discord module blob.
  The Modules service retains the channel master switch; this service owns guild
  channels, roles and feature settings.
- `data.users.deleted` erases the account's bindings and configs through the
  `discord-data` durable. The `discord_data_bus` role consumes BAGEL_DATA and
  dead-letters that subject only, so keep the subscriber-only boot rather than
  `svcboot.MustNATS`.
- Health checks the SQL pool, RPC connection and the data lane.

## Invariants and pitfalls

- One guild has one broadcaster; one broadcaster can own many guilds. Only
  `guild_id` is unique. Rebinding to a different owner requires explicit unbind.
- A guarded unbind checks the current broadcaster so a delayed request cannot
  remove a new owner's binding. Missing bindings are normal successful lookups.
- Config writes verify binding ownership before version details. Expected version
  zero means no existing row; first save becomes 1. Stale or racing saves conflict.
  Keep the conditional `UPDATE ... WHERE version = expected`, not read-then-write.
- `rpc/config.go` rejects a config save with an invalid snowflake, toggle, range or
  voice-text field, and caps the ignored-channel list at `LogIgnoredChannelsMax`.
- Migration is lazy and non-lossy: reading or writing a guild with no module rows
  but a legacy `GuildConfig` backfills rows from `SplitConfig` in a tx (insert on
  conflict do nothing); the legacy row is never deleted. Auto-migrate creates the table.
- Dual-write for rollback: every `ModuleSet` rewrites `GuildConfig.config =
  JoinModules(rows)` in the same tx (legacy version +1, row created at 1 if missing).
  Legacy `ConfigSet` upserts every module row from `SplitConfig` (each version +1);
  legacy `ConfigGet` returns `JoinModules(rows)` with the legacy version once rows exist,
  so toggles read back normalized to on/off. Remove with the legacy verbs.
- XP totals and levels are durable; per-message earning cooldown remains Valkey
  state elsewhere. Derive levels with the existing domain curve on writes.
- Ticket channel ID is globally unique and history outlives its Discord channel.
  Transitions use guild+channel identity to avoid cross-guild access.
- The ticket-open limit is **not** absolute under MySQL READ-COMMITTED: two first
  opens can race when there are no existing rows to lock. `ticket.go` documents it.
- Transcript RPC caps body size at 2 MiB. The same-schema Ent edge cascades the
  transcript when its ticket is deleted. SQLite tests do not exercise MySQL locks.
- Erasure deletes only bindings, configs and module rows whose `updated_at` is not
  after the event's `deleted_at` (plus clock slack), so a redelivered event cannot
  remove a re-registered account's newer rows. XP, tickets and transcripts are
  guild-keyed and are not erased with the owner.
- Keep `ent/runtime` in boot so field defaults/hooks initialize.

## Focused checks

From repository root:

```sh
go test ./app/db/discord ./app/db/discord/repository ./app/db/discord/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/discord/ent/schema
```

Choose `binding_test.go`, `config_test.go`, `module_test.go`, `xp_test.go`,
`ticket_test.go` for repository behavior; `rpc/config_test.go` and `rpc/module_test.go`
pin snowflake and field validation.
CI's Ent generation and race run are in
[main.yml](../../../.github/workflows/main.yml).
