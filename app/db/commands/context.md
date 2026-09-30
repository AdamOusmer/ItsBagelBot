# Commands data service

Agent guide for `app/db/commands`. Read the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md) for terms and neighboring owners.

## Responsibility and boundaries

Owns custom chat command rows, command-use totals, named URL-fetch definitions,
and sealed fetch API keys in MySQL schema `bagel_commands`.
This service stores definitions; Sesame executes chat commands, Gossip performs
external fetches, and Projector builds the shared read projection.
Public commands-page visibility belongs to Users; hiding that page does not
change `Commands.is_active` or disable chat execution.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| `user_id` | Broadcaster's Twitch ID; owner of a command set. |
| Command / `CommandSpec` | One channel's named reply template and execution settings. |
| Name | Bare normalized trigger: lowercase, trimmed, without leading `!`. |
| Alias | Another normalized trigger reaching the same command. |
| Response | Saved reply template; variables and tokens use the root glossary. |
| `perm` | Required chat permission, validated by the domain validator. |
| `allowed_user_id` | Optional particular viewer restriction; zero means absent. |
| `uses` | Signed-int64 execution count; decimal JSON strings preserve all digits. |
| Use batch / `CommandUseBatch` | Durable receipt of one producer batch, committed with its increment. |
| `bump_counter` | Optional named counter to increment when the command executes. |
| Fetch definition / `FetchSpec` | Named HTTPS URL, optional JSON path and key label. |
| Key label | Broadcaster-local address of a sealed API-key row. |
| `last4` | Display suffix derived at seal time; never usable key material. |
| Projection | Secret-free definition views served for downstream read-model repair. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Boot, schema migration, event consumers, RPC subjects and shutdown order. |
| [repository/commands.go](repository/commands.go) | Normalization, cached lists, edit batching, rename/delete and insert-only restore. |
| [repository/uses.go](repository/uses.go) | Atomic use-batch receipt plus guarded increment; retry payload verification. |
| [repository/backfill.go](repository/backfill.go) | Startup migration deriving bump_counter from older bare counter tokens. |
| [repository/fetch.go](repository/fetch.go) | Fetch validation, quotas, synchronous definition writes, rename and reference checks. |
| [repository/fetchkeys.go](repository/fetchkeys.go) | Every operation allowed near plaintext fetch keys; AEAD associated-data binding. |
| [rpc/dashboard.go](rpc/dashboard.go) | Command `list`, `upsert`, `delete`; an upsert with changed `OriginalName` routes to rename. |
| [rpc/fetchdashboard.go](rpc/fetchdashboard.go) | `fetch_list`, `fetch_set_def`, `fetch_set_key`, `fetch_delete`. |
| [rpc/projection.go](rpc/projection.go) | Full commands and fetch-definition read views. |
| [rpc/fetchkey.go](rpc/fetchkey.go) | Internal key-unseal request for the fetch caller. |
| [ent/schema/](ent/schema/) | Commands, CommandUseBatch, FetchDefinition, FetchKey and Migrations fields/indexes/hooks. |

Shared shapes: [commands RPC](../../../internal/domain/rpc/commands/),
[fetch-key RPC](../../../internal/domain/rpc/fetchkey/),
[projection RPC](../../../internal/domain/rpc/projection/),
[data events](../../../internal/domain/event/data/data_events.go),
and [validation](../../../internal/domain/validate/).
The rest of `ent/` is generated; edit schema sources and regenerate.

## Flow and contracts

- Dashboard requests default to `bagel.rpc.commands.*`; user guards parse the
  owner ID before repository work.
- Ordinary command upserts validate and coalesce in a 2-second / 256-item
  write-behind window. Success means accepted; events describe committed state.
- Rename and delete are immediate. Rename keeps the existing row and its use
  count; inspect its pending-write handling before changing this behavior.
- `data.commands.used` is consumed by a durable group. `RecordUse` commits the
  batch receipt and increment in one SQL transaction before acknowledgment.
  Legacy messages fall back to transport UUID when DTO BatchID is absent.
- Command/fetch change broadcasts invalidate every instance's local cached list.
  Grouped user-deletion events sweep commands, fetch definitions and sealed keys.
- Default repair subjects are `bagel.rpc.internal.projection.commands.get` and
  `bagel.rpc.internal.projection.commands.fetches.get`; key reads use
  `bagel.rpc.internal.commands.fetchkey.get`. `main.go` lists environment overrides.

## Invariants and pitfalls

- Unique ownership keys are `(user_id, name)` and `(user_id, label)` for keys.
  Normalize names consistently across requests, events, hooks and references.
- Command-content upserts preserve `uses` and `created_at`. Use increments guard
  against signed-int64 overflow before addition, rolling back the receipt on error.
  Wire readers accept historical numeric uses and decimal strings; writers emit strings.
- Use receipts are permanent. Missing/deleted commands consume the batch without
  recreation; replay after recreation cannot credit the replacement command. See
  [COUNTER_DELIVERY.md](repository/COUNTER_DELIVERY.md) before changing replay/retention.
- Restore inserts only if absent, preserving an existing edit/count. Startup
  bump-counter backfill runs before traffic and records its migration marker.
- Fetch definitions write immediately so quota checks observe persisted rows.
  Deletion checks command-template references unless explicitly forced.
- Deleting a key deliberately leaves definition labels dangling; fetch fails
  closed until a key is restored or the definition is relinked.
- AEAD associated data binds secrets to broadcaster **and** label. Dashboard
  reads expose metadata only; internal unseal results must not be cached/logged.
- Missing optional `TINK_KEYSET_PATH` disables custody while keyless definitions
  work; a present invalid keyset is fatal. Keep `ent/runtime` imported in boot.

## Focused checks

Run from repository root (single root Go module):

```sh
go test ./app/db/commands/repository ./app/db/commands/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/commands/ent/schema
```

Generation is for schema changes. CI regenerates Ent and runs `go test -v -race ./...`
in [.github/workflows/main.yml](../../../.github/workflows/main.yml).
Start with `commands_test.go`, `flush_internal_test.go`, `uses_test.go`,
`uses_wire_test.go`, `backfill_test.go` or fetch tests for the affected behavior.
