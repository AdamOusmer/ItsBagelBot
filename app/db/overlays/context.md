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
| [ent/schema/tts_viewer_block.go](ent/schema/tts_viewer_block.go), [repository/tts_blocks.go](repository/tts_blocks.go) | Streamer's manual TTS viewer blocks, keyed by Twitch user id. |
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
- Accounts get their TTS row with default settings on first read.
- Manual viewer blocks: `rpc.overlays.tts.blocks` lists, `.block` adds
  (idempotent, `viewer_id` is the Twitch user id, `viewer_login` is display
  only) and `.unblock` removes. Each returns the current list. The owner and
  settings reads carry `blocked_viewers` ids, and every block change
  republishes the overlay on `data.overlays.tts.changed` at the same
  revision so Projector refreshes. Account deletion removes blocks by the
  same cutoff.

## Unified overlays

- `overlay` rows hold system and custom overlays: kind, template, name,
  SHA-256 token hash (same hashing as the TTS link), generation, the v2
  document and revision. One system overlay per account and template.
- Boot copies each `powered_overlay` row into a system overlay with the token
  hashed, insert-if-absent and without events; the old table stays readable.
- `rpc.overlays.overlay.{list,add_template,get,save,rotate,delete}` are
  user-scoped; the link is returned once on add and rotate. Owner read for
  Projector: `rpc.internal.overlays.powered.by_token` with `token_hash` set (reply `unified`). Writes publish
  `data.overlays.overlay.changed` (`OverlayChangedDTO`, rotate sets
  `previous_token_hash`). Account deletion removes overlays by the same cutoff.

## Overlay feed

- `livefeed` folds `CHANNEL_EVENTS` (durable consumer, one per service group)
  into the overlays-owned Valkey keys `ovf:{<channel id>}:...` for channels
  that own at least one overlay (cached 60 s). Sums apply once per event id;
  latest-of-each, names and the session slot are version guarded; the
  session event log expires with the session. All-time totals and leaders are
  the `feed_total` counters (`IncrementCounters`), applied once per event id
  in one transaction with a `r.` receipt row (kept 24 h). Subathon time earned
  by an event is recorded in the stored clock by event id. Both run before the
  Valkey receipt, so a failed event is retried whole.
- Owner alpha comes from `rpc.AlphaAccess`, a `pkg/cache` cache dropped on the
  users `status` invalidation; assets, the feed and the lapse sweeper share one.
  Upload starts are throttled by `pkg/ratelimit` (`ratelimit:overlays:upload:`).
- Release order: overlays (stream, ACL, alpha invalidation import) before
  sesame. Sesame runs the feed module only for alpha owners, so a non-alpha
  channel never touches the feed path.
- Each fold queues the event as an alert with `bus.Overlays.Publish` (kind
  `custom`, `event_id` is the Twitch event id, `expires_at` is the
  `bus.OverlayClipTTL` after publish, tier follows the owner's premium plan
  cached 60 s). Stats and, when the timer moved, subathon frames go on core
  NATS `overlays.state.<user id>` as `{"type","data"}`, with no replay. Both
  use the service's existing RPC connection. The `stats` frame carries no
  credit lists; a `credits` frame follows each event with the credit entries it
  touched as their new totals (merge by user id, so a repeat is harmless).
- `rpc.internal.overlays.feed.snapshot` returns the folded state, the full
  credit lists and the newest `events` events of the current session (request
  field `events`, `DefaultSnapshotEvents` when 0; the log expires with the session), so a page
  that connects late catches up without replay; `rpc.overlays.feed.control`
  is owner only and throttles test events.
- Chat is not fed yet: the credits mod, vip and chatter lists stay empty.
- Subathon rules come from the first timer widget in subathon mode
  (`settings.rules`).
- Per-kind widget settings are checked against
  `internal/domain/overlays/testdata/widget-settings.json`, shared with the
  TypeScript runtime. Starter documents live in `templates/` and are added
  with `add_template` (alpha accounts only; custom overlays, assets, imports and
  the live feed are gated on `rpc.internal.users.alpha.check`).

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
