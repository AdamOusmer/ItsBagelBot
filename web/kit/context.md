# Shared web kit context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md).
`@bagel/kit` is a source-exported Bun workspace package shared by the four front ends.

## Responsibility and boundaries

- Owns bot-aware Svelte shells, catalogs, navigation, localization and validation.
- Owns reusable console server infrastructure: RPC, sessions, OAuth, caching and resilience.
- Owns pure TypeScript mirrors of Go template behavior and shared variable metadata.
- Owns supported import-format adapters; dashboard orchestrates persistence/import execution.
- [UI](../../ui/context.md) owns domain-independent primitives; dependency direction is kit → ui.
- Server imports must use explicit `@bagel/kit/server/*` subpaths, never browser bundles.
- Static Astro sites use pure engine/variables subpaths without pulling in server dependencies.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Catalog / ModuleDef | Metadata describing a bot module's fields, command/reply capabilities and UX. |
| Variable manifest | Public shared inventory describing variable identities, aliases, forms and surfaces. |
| Palette | Set of variables permitted by a particular reply field/surface. |
| Rehearsal | Pure template/command preview using sample context; does not execute the live bot. |
| Import adapter | Converts one source bot's exported data into the canonical import model. |
| SWR | Stale-while-revalidate read policy: serve acceptable cached data while refreshing. |
| Cache fabric | App-owned L1/cache-through facade with shared policies and push invalidation. |
| Scope | Invalidation category, or internal resolver group; distinguish these by context. |
| L2 / projection | Valkey-derived settings read; authoritative writes still go to owning services. |
| Wire shape | Service payload format, often snake_case; app-facing views can be camelCase. |

Use root terminology for variable/token/form/surface and all Premium/giveaway concepts.
Catalog metadata, preview samples, and running service behavior are related but not interchangeable.

## Code navigation

- [package.json](package.json): explicit public export map; adding a private file does not publish a subpath.
- [lib/index.ts](lib/index.ts): bot-aware components and shared domain helpers; generic UI stays separate.
- `components/`: RootShell, ConsoleShell, OperatorMenu, notification/permission wrappers and toggles.
  Web code imports generic UI components directly, not through UI or kit barrels.
- [lib/catalog/index.ts](lib/catalog/index.ts) and [module-def.ts](lib/catalog/module-def.ts): module registry/types;
  individual files describe time, alerts, games, moderation, queues, integrations and other modules.
- [lib/module-index.ts](lib/module-index.ts): filter/query/category grouping and route selection for modules.
- [lib/module-copy.ts](lib/module-copy.ts): catalog localization keys and display helpers.
- `lib/nav-core.ts`, `lib/nav-dashboard.ts`, `lib/nav-admin.ts`, `lib/nav.ts`: section registries,
  role-aware navigation and delegate path checks; do not hand-maintain a second route-permission list.
- [lib/variables/index.ts](lib/variables/index.ts): `variableById` and alias-aware `variableByHead` lookups.
- `lib/variables/variables.ts`, `surfaces.ts`, `module-variables.ts`: variable inventory/surface palettes.
- `lib/engine/tmpl.ts`, `pure.ts`, `rehearsal.ts`: token grammar, pure evaluation and preview.
- `lib/engine/commands-validate.ts`, `fetch-validate.ts`, `fetch-tokens.ts`: command/data-source validation.
- `lib/importer/`: adapters for supported bot formats, caps, strategies and canonical import types.
- `lib/i18n/`: runtime/context, tree/flat catalog readers and generated keys. Catalog data lives in
  root `locales/<lang>/{console,modules,website,docs}/`; shared hooks await `ensureCatalog`.
- Static sites use `i18n/flat` or `i18n/fs`; keep server filesystem readers out of browser bundles.
- `styles/`: console-specific theme glue; shared element contracts live in ui.

## Server infrastructure and data flow

- [lib/server/nats.ts](lib/server/nats.ts): `rpc`, `publish`, durable subscriptions, readiness,
  JetStream managers, TLS and local-leaf/hub connection behavior.
- [lib/server/service.ts](lib/server/service.ts): `defineRead`/`defineWrite` adapters;
  default read timeout is 2s, write timeout 5s, overridden for slower integrations.
- `lib/server/cache.ts`, `cache-keys.ts`, `cache-fabric.ts`, `invalidation.ts`: L1 storage,
  policy table, facade and routing; each console supplies its own subjects/scope map/capacity.
- `lib/server/valkey-store.ts`, `valkey-connection.ts`, `songqueue-store.ts`: projected reads/connections.
- [lib/server/session.ts](lib/server/session.ts): shared sealed session codec/key handling;
  app wrappers define their own payloads/cookie names. Normal TTL 7 days; impersonation TTL 1 hour.
- `lib/server/oauth.ts`, `oauth-state.ts`, `impersonation.ts`: OAuth primitives and identity-bound state.
- `lib/server/boot.ts`, `config.ts`, `config-sanity.ts`, `hooks.ts`, `rum.ts`: runtime setup,
  validated configuration, common request/error hardening and New Relic browser injection.
- `rate-limit.ts`, `session-revocation.ts`, `shared-snapshot.ts`, `resilience.ts` and `best-effort.ts`
  solve shared fleet-limits, revocation, read coordination and graceful noncritical reads.
- App route → app adapter → kit RPC/cache infrastructure → owning service; invalidations return to app caches.

## Invariants and generation

- Keep generic components/framework-free browser engines in ui; `verify:ui-only` enforces the boundary.
- Mirror engine grammar and palette availability faithfully; parity tests cross-check Go/shared contracts.
- Do not advertise a variable globally because one surface supports it; surfaces have distinct semantics.
- `catalog/template-namespaces.ts` canonicalizes module reply fields/defaults and samples;
  use its lexer-based migration helpers rather than blindly prefixing every brace token.
- Boot imports cannot touch SvelteKit's dynamic private env proxy before `server.init()` settles.
- Keep catalog/module localization and generated contracts in step when adding capabilities.
- `variables:generate` writes `internal/domain/modulevars/catalog.json` from module variable specs.
- `lib/timers.ts` supplies shared timer defaults/conditions; preserve recent-chat window and per-stream
  semantics instead of treating timers as unconditional fixed-period messages.
- `i18n:keys` regenerates localization key types; inspect outputs rather than hand-editing them.
- Production checks require build-time demo gating; dynamic fixture imports must disappear from server output.

## Focused commands

From `web/kit`: `bun run variables:generate`, `bun run i18n:keys`, `bun run verify:ui-only`.
From `web`: `bun test kit/lib/variables` or `bun test kit/lib/catalog` for those contracts.
`bun run test` runs UI ownership checks, isolated Bun tests across all five web packages,
bundle budgets and compositor-motion checks; it excludes Playwright `.spec.js`.
From `web/kit`: `bun run check` verifies generated English console key types are current.
From `web`: `bun run check` runs workspace package check scripts.
For infrastructure changes run relevant `kit/lib/server/*.test.ts`; dependency-backed tests may skip
without their environment. Pure test success does not establish live NATS/Valkey topology correctness.
