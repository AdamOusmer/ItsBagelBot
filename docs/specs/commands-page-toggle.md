# Public commands page: per-channel off switch

Date: 2026-09-18
Status: Reviewed 2026-09-18, all defaults accepted. Implementation in progress as the stacked PRs in §10.

## 1. Outcome

`!commands` (and its aliases `cmd`, `cmds`, `command`) keeps answering with the channel's public page link, `<PublicBaseURL>/user/<login>`. The dashboard Settings page gains one owner-only toggle, **Public commands page**, on by default. When a broadcaster turns it off:

- `commands.itsbagelbot.com/user/<login>` (and the legacy `/user/<id>` form) answers **404**, indistinguishable from an unknown channel.
- `!commands` stops printing the link and answers with a short "not public" line instead, so chat is never handed a dead URL.
- Moderator subcommands (`!cmd add|edit|remove`) are unaffected. The toggle hides the page; it does not disable commands.

Turning it back on restores both within the cache windows in §7.

## 2. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | The flag lives on the users service account row, next to `locale` and `custom_cursor`, not in a module config blob. | It is an account-level preference read by three consumers (console public page, console settings, sesame). The `custom_cursor` chain already provides schema, RPC verb, console read/write, and invalidation (its coalesced write is the one part not reused, see D8): [user.go:63](../../app/db/users/ent/schema/user.go:63), [preferences.go](../../app/db/users/repository/preferences.go), [dashboard.go:378](../../app/db/users/rpc/dashboard.go:378), [services.ts:527](../../web/dashboard/src/lib/server/services.ts:527). |
| D2 | Stored **inverted**: `commands_page_hidden bool default false`. | Every reader that lacks the field (older Valkey hash, older publisher during rollout, failed read, absent JSON key) resolves to "visible", which is the pre-feature behaviour. No tri-state or `*bool` plumbing, unlike `locale` which needed the "empty means unchanged" rule in [valkey.go:77](../../internal/projection/valkey.go:77). |
| D3 | Disabled page returns `error(404, 'Channel not found')`, the same status and message as an unknown login. | A different message would let anyone probe which channels exist but chose to hide. |
| D4 | `!commands` with the page hidden replies with a one-line i18n message rather than staying silent or printing the link. | Silence looks like the bot is down; the link would 404. Reply text in §6. |
| D5 | Owner-only. Delegates and admin view-as sessions cannot flip it. | Same rule as every other Settings preference ([+page.server.ts:104](<../../web/dashboard/src/routes/(app)/settings/+page.server.ts:104>), [cursor/+server.ts:12](../../web/dashboard/src/routes/cursor/+server.ts:12)). |
| D6 | Sesame reads the flag through the existing user projection (`projection.Reader.User`), failing **open** (link printed) on a read error. | Same read the locale already rides ([dispatch.go:71](../../app/twitch/sesame/engine/dispatch.go:71)); no new RPC from the worker. A projection outage must not silently hide every channel's link. |
| D7 | The toggle purges the page's canonical URL from the Cloudflare edge (both directions, best-effort), and a hidden channel's 404 is edge-cached under the same TTL as the 200. | Without a purge a cached 200 outlives the toggle by up to 60 s stale + 300 s SWR ([hooks.server.ts:127](../../web/dashboard/src/hooks.server.ts:127)). Without caching the 404, every hit on a hidden URL reaches origin and the users RPC, which is exactly the abuse the edge cache exists to absorb. Purge failure degrades to the SWR window and is reported to the user, never hidden. |
| D8 | The flag write is **write-through** (immediate row update + `publishChanged`), not a coalesced preference. | `Users.Get` does not overlay pending preference writes ([users.go:193](../../app/db/users/repository/users.go:193)); a `state_get` inside the 2 s flush window ([preferences.go:24](../../app/db/users/repository/preferences.go:24)) returns the old row and the console caches it fresh for 120 s. Locale and cursor hide that race behind a cookie; a public 404 cannot. `SetBanned` ([users.go:350](../../app/db/users/repository/users.go:350)) is the precedent: low write volume, correctness over batching. |

## 3. Data model (users service)

[app/db/users/ent/schema/user.go](../../app/db/users/ent/schema/user.go), after `custom_cursor`:

```go
// commands_page_hidden turns the public commands page off for this channel.
// Stored inverted so every reader without the field (older hash, older
// publisher, failed read) resolves to visible, the pre-feature behaviour.
field.Bool("commands_page_hidden").Default(false),
```

- Migration: `client.Schema.Create` auto-migrate ([main.go:105](../../app/db/users/main.go:105)) adds the column with the default. Additive, no backfill.
- `repository.UserView` gains `CommandsPageHidden bool \`json:"commands_page_hidden"\`` ([users.go:48](../../app/db/users/repository/users.go:48)).
- Write path (D8): `Users.SetCommandsPageHidden(ctx, id, hidden)` is an immediate `UpdateOneID(id).SetCommandsPageHidden(hidden).Exec` followed by `publishChanged(ctx, id)`, the [SetBanned](../../app/db/users/repository/users.go:350) shape, not the `queuePref` batcher. `publishChanged` drops this replica's `views` entry and announces `UserChanged`; the other users replicas drop theirs through the existing `OnChangeInvalidate` consumer ([main.go:115](../../app/db/users/main.go:115)). The RPC therefore answers only after the row is committed and announced, which is what lets every downstream cache drop-and-refetch safely (§7).
- `data.UserChangedDTO` gains `CommandsPageHidden bool \`json:"commands_page_hidden"\``; `publishChanged` ([users.go:406](../../app/db/users/repository/users.go:406)) sets it from the view.

## 4. RPC surface

### 4.1 Dashboard verbs (`bagel.rpc.users.dashboard.*`, [dashboard.go:44](../../app/db/users/rpc/dashboard.go:44))

- New verb `commands_page_set`, request `usersrpc.CommandsPageSetRequest{ BroadcasterUserID string; Hidden bool }` in [internal/domain/rpc/users/users.go](../../internal/domain/rpc/users/users.go) next to `CursorSetRequest`. Handler is one `setBoolPref` call with scope `"commands_page"`, op `"commands_page_set"`, mirroring `handleCursorSet`. `setBoolPref` runs the repo write and only then publishes the invalidation ([dashboard.go:117](../../app/db/users/rpc/dashboard.go:117)), so with the write-through repo method the scope broadcast always trails the commit.
- `state_get` reply map ([dashboard.go:274](../../app/db/users/rpc/dashboard.go:274)) gains `"commands_page_hidden": view.CommandsPageHidden`.
- Invalidation scope `commands_page` is published on write; the console fabric drops `commands_page:<id>` (§5.1).

### 4.2 Projection

- `projection.UserReply` and `projection.User` ([client.go:63](../../internal/projection/client.go:63)) gain `CommandsPageHidden bool \`json:"commands_page_hidden,omitempty"\``. Users' projection verb ([projection.go:22](../../app/db/users/rpc/projection.go:22)) fills it from the view.
- `projection.UserProjection` and `SetUserWithTTL` ([valkey.go:61](../../internal/projection/valkey.go:61)) write hash field `commands_page_hidden` as `utils.BoolField`, unconditionally (D2 makes the locale-style "skip when empty" rule unnecessary). `GetUser` returns it; absent field reads as false.
- Projector `applyUserChanged` ([projector.go:136](../../app/projector/projector.go:136)) and the status RPC write-back ([status.go:129](../../app/projector/rpc/status.go:129)) copy the field through.
- `applyUserChanged` additionally publishes `<cacheInvalidatePrefix>.status` for the user **after** the Valkey write, the way commands/modules folds already do for their scopes. Sesame's listener already maps `status` to a `User` eviction ([client.go:286](../../internal/projection/client.go:286)); the console maps it to account/tier/ban keys, which is a correct drop for any user change. This closes the race where sesame evicts on the users-service invalidation, re-reads the hash before the projector has folded the event, and caches the old value for a full 30 s TTL. No new subject, no new consumer.

## 5. Console (`web/dashboard`)

### 5.1 Reads and writes

[services.ts:523](../../web/dashboard/src/lib/server/services.ts:523), same shape as locale/cursor:

```ts
// Page is public unless the account explicitly hid it; an absent field
// (older users service, failed read) keeps the pre-feature behaviour.
export const userCommandsPage = prefRead('commands_page', (r) => r.commands_page_hidden !== true);
export const setCommandsPage = prefWrite<boolean>('commands_page_set', 'commands_page_hidden', 'commands_page');
```

`prefRead` keys the cache `commands_page:<id>` under `POLICY.entity` (120 s fresh, 600 s SWR, [cache-keys.ts:38](../../web/kit/lib/server/cache-keys.ts:38)). Two registrations make the scope reach every replica, and both are required:

- `SCOPES` ([services.ts:64](../../web/dashboard/src/lib/server/services.ts:64)): `commands_page: (id) => [\`commands_page:${id}\`]`. Without it the bus router falls through to `*`, which only flushes `userPrefixes` and would miss the key.
- `userPrefixes` ([services.ts:57](../../web/dashboard/src/lib/server/services.ts:57)): append `commands_page:${id}` so the coarse `*` flush and `invalidateUser` cover it too.

The writer replica also drops its own key synchronously through `prefWrite`'s `after`, so the settings page re-render after the action reads fresh.

### 5.2 Settings page

[routes/(app)/settings/+page.server.ts](<../../web/dashboard/src/routes/(app)/settings/+page.server.ts>):

- `load` adds `userCommandsPage(self)` to the `Promise.allSettled` batch ([:110](<../../web/dashboard/src/routes/(app)/settings/+page.server.ts:110>)); a rejected read returns `commandsPage: true` and sets `degraded`. DEMO branch returns `commandsPage: true`.
- New action `setCommandsPage`: owner guard (`!s || s.delegate_of || s.impersonator_id` → 403), reads `enabled` from form data, calls `setCommandsPage(s.user_id, !enabled)`; 502 on failure with the usual "Could not update. Try again in a moment." string. Follows the `markRead` action shape ([:236](<../../web/dashboard/src/routes/(app)/settings/+page.server.ts:236>)); no cookie mirror, because nothing on the client renders differently.
- After a successful write the action calls `purgeEdge([commandsHref(login)])` (§7.2) and returns `{ ok: true, action: 'commands_page', edgeDelayed: boolean }`. `edgeDelayed: true` (purge failed or not configured) makes the page show `settings.commandsPageDelayed` under the switch. The login comes from `accountState(s.user_id).username`, lower-cased, the same value `canonicalLogin` redirects to; only that exact URL is ever cached.

[+page.svelte](<../../web/dashboard/src/routes/(app)/settings/+page.svelte>): a new **Public pages** section placed after the custom-cursor pref ([:484](<../../web/dashboard/src/routes/(app)/settings/+page.svelte:484>)), one switch row with label `settings.commandsPage`, hint `settings.commandsPageHint` (shows the channel's own URL), enhanced form posting to `?/setCommandsPage`. Reuse the existing pref-row markup and switch primitive from `web/kit`; no new component.

### 5.3 Public page

[routes/(public)/user/[channel]/+page.server.ts:112](<../../web/dashboard/src/routes/(public)/user/[channel]/+page.server.ts:112>), right after `resolveChannel` and before the `Promise.all`:

```ts
// Hidden channels 404 with the unknown-channel message so a probe cannot
// tell "no such channel" from "chose not to publish" (spec D3). The locals
// flag lets hooks edge-cache THIS 404 (spec D7); an unknown login stays
// no-store so a channel that enrolls a minute later is not stuck behind a
// cached miss.
if (!(await userCommandsPage(userId).catch(() => true))) {
  locals.edgeCache404 = true;
  throw error(404, 'Channel not found');
}
```

[hooks.server.ts:159](../../web/dashboard/src/hooks.server.ts:159) `edgeCacheControl` gains one clause: a 404 is cacheable when `event.locals.edgeCache404` is set; every other gate (GET/HEAD, anonymous, `en`, no `?lang`, no cursor opt-out) applies unchanged. `App.Locals` gets `edgeCache404?: boolean`. The `+error.svelte` render for that request carries no per-visitor state, same as the 200.

- Placed after `resolveChannel` on purpose: the legacy `/user/<id>` path still 308s to the login form first, then 404s. The login is already public on Twitch, so the redirect leaks nothing.
- The read failing open (`catch(() => true)`) matches D6: a users-service blip degrades to the current behaviour, not to a wall of 404s.
- DEMO branch: unchanged (page always renders).
- [sitemap.xml](../../web/dashboard/src/routes/sitemap.xml/+server.ts) lists no `/user/<login>` pages today (`commands: []`), so nothing to filter.
- Leaderboard hero link ([(public)/[user]/+page.svelte:41](<../../web/dashboard/src/routes/(public)/[user]/+page.svelte:41>)) keeps linking; see Q2.

## 6. Sesame (`app/twitch/sesame/modules/cmd.go`)

[cmdLink](../../app/twitch/sesame/modules/cmd.go:170) gains the gate before building the URL. `engine.Deps` already carries `Proj projection.Reader`:

```go
// Fail open: a projection miss prints the link (spec D6). A hidden page
// answers with a one-liner instead of a URL that would 404.
if u, err := d.Proj.User(ctx, broadcasterID); err == nil && u.CommandsPageHidden {
    emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID,
        Text: strings.NewReplacer("{user}", c.Env.ChatterName(), "{channel}", channel).Replace(i18n.T(c.Locale, "cmd.page_off"))})
    return
}
```

- `cmdLink` needs `ctx` threaded from the `Run` closure (three call sites in the same file). Keep the function under the CodeScene ceiling: split the "hidden" reply into `cmdPageOff(...)` if the branch pushes `cmdLink` past 9.
- `!cmd add|edit|remove` for moderators: untouched.
- `!cmd` from a viewer who tries a mod subcommand today falls through to `cmdLink` ([cmd.go:58](../../app/twitch/sesame/modules/cmd.go:58)); with the page hidden they get the same one-liner. Acceptable.

## 7. Caching

Every layer that holds the flag, what drops it, and the worst-case delay after a toggle. Verified against the code, not the comments.

### 7.1 Layer table

| Layer | Holds | TTL / policy | Dropped by | Worst case after toggle |
| --- | --- | --- | --- | --- |
| Users repo `views` (per replica) | `UserView` | in-process | writer: `publishChanged` inline; others: `UserChanged` consumer ([main.go:115](../../app/db/users/main.go:115)) | one core-NATS hop (ms) |
| Console fabric (per replica) | `commands_page:<id>` | 120 s fresh / 600 s SWR | writer: `prefWrite.after`; others: bus scope `commands_page` (§5.1) | one bus hop; the drop trails the commit (D8) so the refetch never re-caches the old row |
| Cloudflare edge | `/user/<login>` 200 or 404 HTML, anonymous `en` only | `s-maxage=60, stale-while-revalidate=300` | `purgeEdge` from the settings action (§7.2) | purge OK: next request; purge failed: ≤ 360 s, surfaced as `edgeDelayed` |
| Valkey user hash | `commands_page_hidden` field | 24 h ([valkey.go:34](../../internal/projection/valkey.go:34)) | projector fold of `UserChanged` | JetStream hop (ms) |
| Sesame in-process | `projection.User` | 30 s ([main.go:31](../../app/twitch/sesame/main.go:31)) | scope `status` published by the projector **after** its Valkey write (§4.2) | one hop after the fold; 30 s TTL is the ceiling if the invalidation is lost |
| Browser | nothing | `max-age=0` + kit ETag | n/a | next navigation revalidates |

Ordering guarantee that makes the table hold: RPC answers after commit (D8) → users invalidation scope trails the commit → console refetch sees the new row. `UserChanged` is published inside the same call → projector folds → projector publishes `status` after the hash write → sesame refetch sees the new hash. The users-service `commands_page` scope is also mapped in sesame's `evictScope` (added to the `"status", "grant", "live", "locale"` case, [client.go:286](../../internal/projection/client.go:286)) so a lost projector message still converges within one TTL rather than logging "unknown scope".

### 7.2 Edge purge

New helper `web/dashboard/src/lib/server/edge-purge.ts`:

```ts
// Best-effort Cloudflare purge-by-URL. Returns false (never throws) when the
// zone/token are unset or the API refuses: the caller falls back to the SWR
// window and tells the user, it does not fail the write that already landed.
export async function purgeEdge(urls: string[]): Promise<boolean>
```

- `POST https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/purge_cache` with `{ files: urls }`, bearer `CF_CACHE_PURGE_TOKEN`, 3 s timeout. Purge-by-URL is available on every plan and matches the default cache key (scheme + host + path), which is the key these pages use.
- Secrets: `CF_ZONE_ID`, `CF_CACHE_PURGE_TOKEN` in the console-dashboard Doppler config; token scope `Zone → Cache Purge → Purge` on the itsbagelbot.com zone only. The existing API token lacks this scope; mint a dedicated one rather than widening it.
- Purged URL set: `[commandsHref(login)]`. The legacy `/user/<id>` form 308s and is never cached; `?lang` variants are never cached; non-`en` renders are never cached. One URL covers everything the edge can hold.
- DEMO: `purgeEdge` returns true without calling out.

### 7.3 Rollout order

Users service (schema, write-through, publisher) → projector (field + post-write `status` scope) → sesame (gate, `evictScope` mapping) → console (toggle, 404, purge, hooks). Until the projector ships the hash lacks the field and sesame reads "visible"; until the console ships nothing can set it; until the Doppler secrets land `edgeDelayed` is always true and the page tells the owner so. No window produces a wrong 404.

## 8. i18n

Console, [web/kit/lib/i18n/locales/{en,fr}.json](../../web/kit/lib/i18n/locales/en.json):

| Key | EN | FR |
| --- | --- | --- |
| `settings.publicPages` | Public pages | Pages publiques |
| `settings.commandsPage` | Public commands page | Page publique des commandes |
| `settings.commandsPageHint` | Viewers can open {url}. Off: that link answers 404 and !commands stops sharing it. | Les spectateurs peuvent ouvrir {url}. Désactivée : ce lien répond 404 et !commands cesse de le partager. |
| `settings.commandsPageDelayed` | Saved. The public link can take up to 6 minutes to catch up. | Enregistré. Le lien public peut mettre jusqu'à 6 minutes à se mettre à jour. |

Chat, [internal/domain/i18n/locales/{en,fr}.json](../../internal/domain/i18n/locales/en.json):

| Key | EN | FR |
| --- | --- | --- |
| `cmd.page_off` | @{user} {channel}'s command list isn't public. | @{user} la liste des commandes de {channel} n'est pas publique. |

All console keys are literal (resolved by [literal-keys.test.ts](../../web/kit/lib/i18n/literal-keys.test.ts)); add FR in the same commit, no parity gate exists to catch a miss.

## 9. Tests

- **users repository**: `SetCommandsPageHidden` writes through and `Get` on the same replica returns the new value immediately (no batcher window); `publishChanged` DTO carries the field.
- **users rpc**: `commands_page_set` round-trip mirrors the `cursor_set` test; `state_get` includes `commands_page_hidden`.
- **projection valkey**: `SetUser`/`GetUser` round-trips the field; hash without the field reads false.
- **projector**: the field copy-through is covered by the projection store round-trip; the post-write `status` publish is ordered by position in `applyUserChanged` and documented there (a recording-Valkey + live-NATS ordering test was tried and dropped: 280 lines of RESP handshake fakery for an env-gated test CI never runs).
- **projection client**: `evictScope("commands_page")` drops the user entry.
- **sesame cmd**: hidden → `cmd.page_off` text, no URL in output; visible → unchanged link; projection error → link (fail open).
- **console**: `(public)/user/[channel]` load test: hidden → 404 with `Channel not found` and `locals.edgeCache404 === true`; unknown login → 404 without the flag; read rejection → 200. `edgeCacheControl`: 404 + flag → cache header; 404 without flag → null; flag + session → null. Settings action: delegate → 403; owner `enabled=on` → `setCommandsPage(id, false)` then `purgeEdge([url])`; purge false → `edgeDelayed: true`. `SCOPES.commands_page` routes to the key; `userPrefixes` includes it. `purgeEdge`: unset env → false, non-2xx → false, timeout → false.
- **i18n**: literal-keys test picks up the three new console keys.

## 10. PR split (stacked, bottom-up)

1. `feat/users-commands-page-flag`: schema, write-through repo method, DTO, dashboard verb, state_get, projection verb, `projection.User`/`UserReply`/valkey store, projector copy-through + post-write `status` scope, `evictScope` mapping. Go only, additive, deployable alone.
2. `feat/sesame-commands-page-gate` (on 1): `cmd.go` gate + `cmd.page_off` EN/FR.
3. `feat/console-commands-page-toggle` (on 1): services read/write + `SCOPES`/`userPrefixes`, settings load + action + section, public page 404 + locals flag, hooks 404 clause, `edge-purge.ts`, console EN/FR. Doppler secrets land before this deploys.

Each clears CodeScene on its own; 2 and 3 are independent of each other.

## 11. Out of scope

- Hiding the channel from any directory or leaderboard surface (Q2).
- Per-command visibility (already exists on the command itself; unchanged).
- Purging anything beyond the one canonical URL (leaderboard, stats).
- A public "this channel is private" page instead of 404 (D3 rejects it).

## 12. Resolved questions (2026-09-18)

All defaults accepted: one-liner reply when hidden (D4); leaderboard keeps its link; owner only; toggle reads "Public commands page", on = public; hidden 404 is edge-cached and purged on toggle (D7); projector post-write `status` scope (§4.2); legacy id links redirect then 404; fail open on read errors (D6). Vocabulary recorded in [CONTEXT.md](../../CONTEXT.md): commands page, public / hidden.
