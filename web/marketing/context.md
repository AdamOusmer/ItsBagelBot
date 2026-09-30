# Marketing site context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md).
This Astro package builds the public product site, localized guides and command workbench.

## Responsibility and boundaries

- Presents features, pricing, integrations/import support, comparisons, contact and legal content.
- Builds localized static pages; the primary production origin is `https://itsbagelbot.com`.
- The command builder rehearses templates in-browser and links to the authenticated dashboard.
- It does not persist broadcaster settings, fulfill Premium purchases or execute live bot commands.
- [Kit](../kit/context.md) supplies shared link registries, variables and pure preview engines.
- [UI](../../ui/context.md) supplies Astro visual adapters, CSS contracts and browser effects.
- Architecture/service/operator documentation belongs to [docs](../docs/context.md).

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Locale / Lang | Catalog-discovered language code; English is unprefixed, others use `/<code>/`. |
| Localized path | Page with translated twins used by routing, switchers and hreflang emission. |
| Guide structure | English typed content describing blocks/screens; translations replace copy. |
| Guide strings | Locale-specific copy mapped onto that fixed guide structure. |
| Screen | Static instructional depiction of a dashboard area, not the live dashboard component. |
| Command builder | Public custom-command/module-reply composer with sample chat rehearsal. |
| Recipe | Example configuration for the builder, separate from persisted broadcaster state. |
| JSON island | Embedded data read by client code; used for CSP-compatible localized configuration. |
| Changelog entry | One validated JSON release record, keyed/sorted by its `version` field. |
| Legal section | Ordered Markdown section; filename suffix supplies its stable anchor. |

Use the root variable/token/form/surface vocabulary and distinguish Premium access from subscriptions.
Preview samples illustrate behavior; availability and variable grammar come from shared kit metadata.

## Start here for changes

- [astro.config.mjs](astro.config.mjs): locale discovery, route policy, sitemap alternatives,
  production origin, CSP-compatible asset externalization and stylesheet build settings.
- [Layout.astro](src/layouts/Layout.astro): shared chrome, canonical/hreflang/schema metadata,
  font preloads, transition router, and build-time content guards.
- [src/i18n/ui.ts](src/i18n/ui.ts): locale catalogs, `localeStaticPaths`, URL helpers,
  `LOCALIZED_PATHS`, dashboard links and English fallback behavior.
- Root `locales/<lang>/website/**/*.json`: site strings discovered via kit flat/fs catalog readers;
  `src/i18n/lang.ts` names the default, and `builder.ts` resolves workbench strings/recipes.
- [src/content.config.ts](src/content.config.ts): schemas/loaders for legal metadata/sections and changelog.
- [contentGuards.ts](src/lib/contentGuards.ts): content/locale safety checks imported by every layout.
- [lib/guides/registry.ts](src/lib/guides/registry.ts): `getGuide`, `getHub`, slugs and build-time parity checks.
- `src/lib/guides/types.ts`, `translate.ts`, `parity.ts`, `slugs.ts`: guide shape/copy contracts and URLs.
- `src/content/guides/`: `*.en.ts` guide structures, locale JSON strings and `hub.<lang>.json`.
- `src/components/guides/blocks/` renders prose, steps, tables, cards and callouts;
  `screens/` renders instructional dashboard depictions.
- [CommandBuilder.astro](src/components/builder/CommandBuilder.astro): composer UI and client behavior.
- `src/lib/variables/`: marketing presentation adapters over shared variable inventory.
- `src/script/`: navigation bindings, reveal/scroll effects, WebGL geometry and other site interaction.
- `src/styles/style.css`: app styling layered over shared UI brand/element contracts.

## Route/content areas and data flow

- `src/pages/[...lang]/` emits home, pricing, contact, import, song requests and Valorant pages.
- `guides/index.astro`, `guides/[slug].astro`, `guides/variables.astro` emit tutorial/reference pages.
- `command-builder.astro` exposes the workbench; `changelog/` includes index and version pages.
- `terms.astro`, `privacy.astro`, `creator-terms.astro` assemble structured legal documents.
- `src/pages/vs/` contains comparison landing pages; not every page belongs in localized-path metadata.
- Legal data: `src/content/legal/<document>/<locale>/meta.json` plus `NN-<anchor>.md`.
- Release data: `src/content/changelog/*.json`; localized text supports English fallback.
- Astro build → validated data/catalogs → static HTML/assets → client enhancement after navigation.
- Builder → kit lexer/rehearsal → local chat rendering → copy template/chat command or dashboard deep link.

## Invariants and pitfalls

- Discover locales from JSON files; route and runtime catalogs must agree on the unprefixed default.
- Add translated paths consistently with `LOCALIZED_PATHS` so switchers and hreflang stay correct.
- Preserve guide structure/copy parity; the registry refuses builds with drifted locale content.
- Preserve legal filename suffixes (anchors) and explicit typography; smartypants is disabled there.
- Changelog version sorting uses `version`, not loader IDs or filenames stripped of dots.
- Keep scripts external for production CSP; stylesheets are external too (`inlineStylesheets: never`).
  Client copy/data should use existing safe island/DOM patterns.
- ClientRouter navigations need appropriate effect cleanup/reinitialization; avoid accumulating listeners.
- Do not hand-copy the template evaluator/token regex; use kit's engine subpaths.
- Frontend product claims need reconciliation with real service capabilities when behavior changes.

## Focused commands

From `web/marketing`: `bun run dev`, `bun run build`, `bun run preview` (preview builds first).
For browser tests from `web/marketing`: build first, then `bunx playwright test`.
[playwright.config.js](playwright.config.js) serves existing `dist/` on port 4399 and uses `tests/`.
This package has no `check` or `test` script; do not infer those from workspace-wide scripts.
`tests/site.spec.js` checks rendered site behavior; `tests/compareVersion.spec.js` covers version comparison.
