# Overlays data service

Agent guide for `app/db/overlays`. See the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns each account's Powered-by and TTS overlay links and settings in the
MySQL schema selected by `DB_SCHEMA` (`overlays`). Every write publishes the
full overlay on the DATA stream so Projector can fold it into its read model;
Projector answers the public overlay page (through the controls gateway) and
resolves Premium from the projected account. This service never serves the
overlay page itself.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| Powered overlay / `PoweredOverlay` | One row per account: link token, settings and revision. |
| Link token | 32 random bytes, unpadded base64url (43 chars), stored in plaintext, unique, case-sensitive. |
| Rotate | Replace the link token; the old token stops resolving. |
| Revision | Starts at 1 and increases by one on every save or rotate; the save RPC checks it. |
| Directions | Entrance edges stored as a bitmask in `overlaysrpc.Directions` order. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Boot, account-deletion consumer, RPC surfaces, health. |
| [repository/overlays.go](repository/overlays.go) | Get-or-create, save, rotate, lookups, account deletion and change events. |
| [repository/view.go](repository/view.go) | Row to wire conversion and direction bitmask. |
| [rpc/powered.go](rpc/powered.go) | Dashboard and owner RPC handlers and refusal codes. |
| [ent/schema/powered_overlay.go](ent/schema/powered_overlay.go) | Powered-by fields, unique user and token, binary collation. |
| [ent/schema/tts_overlay.go](ent/schema/tts_overlay.go), [repository/tts.go](repository/tts.go) | Account TTS settings, token hash, generation and revision. |
| [tts.go](tts.go), [rpc/tts.go](rpc/tts.go) | Startup backfill, ban handling, dashboard writes and Projector cold reads. |

Shared wire shapes: [internal/domain/rpc/overlays](../../../internal/domain/rpc/overlays/).
Change event: `data.PoweredOverlayChangedDTO` in [internal/domain/event/data](../../../internal/domain/event/data/).
Other `ent/` files are generated from the schema.

## Flow and contracts

- Dashboard (user-scoped `user_id`): `rpc.overlays.powered.get` creates the row
  with a fresh token and default settings on first read; `.save` validates with
  `PoweredConfig.Validate` (`invalid`) and the read revision (`conflict`);
  `.rotate` issues a new token. Each returns `PoweredView` with the full link.
- Owner read for Projector: `rpc.internal.overlays.powered.by_token` (`token`)
  returns `OwnerPoweredReply`, refusing `not_found` when absent.
- Creation, save and rotate publish `data.overlays.powered.changed` with the
  whole overlay; rotate also sets `previous_token`. Account deletion
  (`data.users.deleted`) deletes the row and publishes `deleted` with the
  removed token in `previous_token`.

## Account TTS

- `rpc.overlays.tts.get` creates default settings with no link. `.save`
  validates the selected voice and clip length and requires the current
  revision. `.rotate` checks Users' voice plan and returns the new OBS URL once.
- The row holds the SHA-256 hash of the token and the cookie generation.
  Writes publish the full row on `data.overlays.tts.changed`. Projector
  updates the lookup cache and broadcasts revocations after the fold.
- Account deletion uses the same cutoff as Powered-by. A ban clears the link
  hash and advances its generation. Alert rules and chat-command settings
  stay in their module configurations.
- Before serving TTS, each replica pages `rpc.internal.users.voice.export`
  and inserts legacy rows that have no TTS overlay. Default-only accounts
  get their rows on demand. The backfill preserves settings, hashes and
  generations, and can restart without overwriting new writes.

## Invariants and pitfalls

- The token column uses `utf8mb4_bin`; a case-insensitive collation would let
  a case variant of a link resolve.
- Deletion only removes rows created before the deletion cutoff, so a
  re-registered account keeps its new link.
- Projector's cold reads through the owner RPCs refill missing or expired
  entries if a change event failed to arrive.

## Focused checks

From repository root:

```sh
go test ./app/db/overlays/...
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/overlays/ent/schema
```
