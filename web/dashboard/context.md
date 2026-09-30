# Broadcaster dashboard context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md).
This Bun/SvelteKit package owns broadcaster configuration UX and public channel pages.

## Responsibility and boundaries

- Authenticated broadcasters configure commands, modules, integrations and Premium billing.
- Delegates operate permitted sections of an owner's board; staff can inspect through view-as.
- Public routes expose channel command directories, statistics and status without a login.
- Server routes call owning Go/Elixir services; the dashboard is not the bot execution engine.
- [Kit](../kit/context.md) owns catalogs, variable inventory, shared server infrastructure and shells.
- [UI](../../ui/context.md) owns reusable presentation contracts and browser engines.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Board / effective ID | Broadcaster whose configuration is being read/written; for delegates, the owner. |
| Session user | Signed-in Twitch identity; may differ from the board owner. |
| Delegate | User with a section-limited grant over another broadcaster's board. |
| Impersonator | Staff actor recorded on a short-lived view-as session. |
| Module definition | Catalog metadata: fields, replies, commands, route and feature gates. |
| Module blob | Persisted `{ enabled, configs }` settings for one board/module. |
| Custom command | Broadcaster-defined trigger/replies; separate from built-in module commands. |
| Reply surface | Context such as custom command, follow alert, or reward reply; determines variable availability. |
| Fetch / data source | Validated external-data definition used by command templates. |
| Commands page | One channel's public command directory; hiding it does not disable commands. |
| Account status | Wire values `free`, `paid`, `vip`; paid or VIP yields Premium access. |
| Timer | Scheduled reply with interval, recent-chat, offline, per-stream-limit and end-time conditions. |

Premium access, Tebex subscription, and promotional awards are distinct in the root glossary.
Preserve variable/token/form/surface wording in editors and help copy.

## Start here for changes

- [hooks.server.ts](src/hooks.server.ts): runtime boot, session guard, global user rate limits,
  locale, response hardening, and carefully restricted anonymous edge caching.
- [guard.ts](src/lib/server/guard.ts): account ban/deletion, delegation revocation and route-scope gates.
- [board.ts](src/lib/server/board.ts): `effectiveId(session)` selects owner ID; missing real session redirects.
- [services.ts](src/lib/server/services.ts): subject registry, wire adapters, account/billing/delegation
  reads, mutation invalidation, and cache fabric (default L1 capacity 1,000 entries).
- [module-page.ts](src/lib/server/module-page.ts), [module-action.ts](src/lib/server/module-action.ts),
  [module-gate.ts](src/lib/server/module-gate.ts): shared load/action paths and Premium/beta/delegate gates.
- `module-flags.ts` derives shell/built-in enable state from saved module rows plus catalog defaults.
- [module-blob.ts](src/lib/server/module-blob.ts): common module persistence shape.
- `src/lib/server/*-store.ts`: focused commands, quotes, timers, fetches, Discord, Spotify,
  Govee, loyalty and channel-point adapters; reuse them rather than writing route RPCs.
- `src/lib/components/`: feature editors; `commands/CommandEditor.svelte` and `ResponseEditor.svelte`
  handle command editing, while `modules/ReplyEditor.svelte` handles module reply surfaces.
- [timers-parse.ts](src/lib/server/timers-parse.ts): timer message/condition normalization;
  [timers-store.ts](src/lib/server/timers-store.ts) persists the timers module blob, enabling the first timer.
- `commands-bulk.ts` and `command-conflict.ts`: batch edits and command trigger/alias conflict checks.
- `songqueue-live.ts`, `songqueue-view.ts`, `live-counters.ts`: bounded live feature read paths.
- [public-directory.ts](src/lib/server/public-directory.ts): public-safe command/module projections.
- [live-hub.ts](src/lib/server/live-hub.ts): per-board invalidation fanout; `routes/events/+server.ts`
  exposes owner-only SSE with constant invalidation payloads, without revealing scope names.

## Route areas and data flow

- `(app)/+layout.server.ts` supplies shell/account data and login redirect UX.
- `(app)/+page.*` is overview; `overview/stream/` feeds stream details.
- `commands/`, `modules/`, `modules/[id]/`, `quotes/`, `timers/`, `counters/`, `channelpoints/`
  cover configuration; dedicated module pages coexist with generic module forms.
- `discord/[guildId]/` splits guild configuration into settings, channels, announcements,
  community, roles and tickets; Discord setup RPC belongs to `app/discord/outgress`, using
  historical `bagel.rpc.dingress` subjects, rather than the Twitch outgress service.
- `songqueue/`, `govee/`, `loyalty/`, and Spotify connect/callback handle their integrations.
- `settings/` and `settings/import/` handle preferences, deletion and supported bot imports.
- `welcome/` and `welcome/import/` complete onboarding before ordinary use.
- `billing/` reads entitlement state and obtains Tebex-hosted checkout through transactions RPC.
- `auth/*`, `delegate/*`, `lang`, and `cursor` manage identity/context/preferences.
- `(public)/user/[channel]/` resolves Twitch login to authoritative account ID for commands pages.
  `(public)/[user]/` serves loyalty leaderboards on the leaderboard hostname (and dev) only.
  Public stats/status and readiness routes have their own policy.
- Read: session guard → effective owner → L1 cache → optional Valkey projection → service RPC.
- Write: authorize/validate → service RPC → invalidate keys → SSE refresh for open owner boards.

## Invariants and pitfalls

- Enforce gates in hooks/actions/API endpoints; layout loads do not cover every request.
- Resolve board identity with `effectiveId`; never fall back to a real-looking `demo` ID in production.
- Avoid session identity when mutating delegated board data; permission checks still use the actor.
- Public labels come from resolved account records, not caller-provided query strings.
- Hidden commands pages answer as missing; invalidation/edge purge must accompany visibility changes.
- Boot import graphs use `process.env`; request-time dynamic env is safe after initialization.
- `DEMO` branches require direct build-time `dev` gating and production cleanup verification.
- Edge-cache only anonymous default renders using normalized Accept-Language; locale preference cookies,
  `?lang`, cursor opt-out and sessions bypass shared caching. Responses vary on Accept-Language.
- Template parsing/rehearsal comes from kit; do not invent a separate token regex or resolver.

## Focused commands

From `web/dashboard`: `bun run dev`, `DEMO=1 bun run dev`, `bun run check`, `bun run build`.
From `web`: `bun test dashboard/src/lib/server/guard.test.ts` for gates,
`bun test dashboard/src/lib/server/services.test.ts` for adapter/invalidation contracts,
or `bun test dashboard/src/hooks.server.test.ts` for public caching behavior.
Workspace `bun run test` runs isolated Bun tests across kit/admin/dashboard/marketing/docs,
excluding Playwright `.spec.js`, plus UI-boundary, size and compositor-motion gates.
Real OAuth/NATS/Valkey integration needs configured dependencies; dev demo is a UI fixture path.
