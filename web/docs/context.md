# Documentation site context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md).
This is the Astro documentation package, deployed as a static site.

## Responsibility and boundaries

- Publishes architecture, infrastructure, service contracts, operating guides and decision history.
- Canonical production origin is configured as `https://docs.itsbagelbot.com`.
- Markdown is rendered at build time; there is no service database or authenticated console.
- Runtime behavior belongs to the relevant app/service code; documentation must be checked against it.
- Broadcaster tutorial/marketing content primarily belongs to [marketing](../marketing/context.md).
- Design contracts belong to [ui](../../ui/context.md); only the page grid and rendered-Markdown look stay here.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Docs collection | Astro content collection loaded from `src/content/docs/`. |
| Space | One of the five top-level sections (Start, Services, Platform, Reference, Records). A space's sidebar lists only its own pages. |
| Reference | Current shape, contracts and inventories used while implementing changes. |
| Architecture | System boundaries, ownership and communication model. |
| Data & State | Persistence/projection/cache model across services. |
| ADR | Numbered record explaining a consequential architectural decision. |
| QA report | Evidence from a particular review or validation run; not a promise of current correctness. |
| Mermaid diagram | Code-defined architecture illustration rendered through the Mermaid integration. |

Root glossary terms apply to product concepts; keep code naming distinct from user-facing language.
An old ADR describes the decision at the time; it may have been superseded by later code/decisions.

## Code and content navigation

- [astro.config.mjs](astro.config.mjs): production origin, code highlight theme, Mermaid palette/layout,
  sitemap and strict Mermaid security mode. Locales come from root `locales/<lang>/docs/`; English is unprefixed.
- [src/content.config.ts](src/content.config.ts): collection loader and frontmatter schema.
- [src/lib/spaces.ts](src/lib/spaces.ts): which page sits in which space, group and order.
- [src/lib/pages.ts](src/lib/pages.ts): per-locale page lookup, with English fallback for untranslated pages.
- [src/i18n/ui.ts](src/i18n/ui.ts): docs catalogs from root `locales/*/docs/`, the marketing footer copy and the language-switch options.
- [src/layouts/Base.astro](src/layouts/Base.astro): head, shared `Nav` and `Footer`, search, cursor and reading progress.
- [src/pages/[...slug].astro](src/pages/[...slug].astro): the route for every Markdown page.
- [src/components/Home.astro](src/components/Home.astro): the home page, rendered for each locale.
- [src/styles/docs.css](src/styles/docs.css): page grid. [src/styles/prose.css](src/styles/prose.css): rendered Markdown.
- [system-overview.md](src/content/docs/reference/system-overview.md): start for the current service
  topology/data plane, then confirm the particular contract in source.
- [rpc-contracts.md](src/content/docs/reference/rpc-contracts.md): service RPC surface orientation.
- [architecture/index.md](src/content/docs/architecture/index.md): external dependencies and boundaries.
- [microservices/index.md](src/content/docs/microservices/index.md): links to individual service ownership.
- [data-and-state/index.md](src/content/docs/data-and-state/index.md): persistence, projection and caching.
- `src/content/docs/infrastructure/`: deployment, networking and performance material.
- `src/content/docs/guides/`: contributor/operator onboarding guides.
- `src/content/docs/qa/`: recorded experiments, canaries and review reports.
- [adr/index.md](src/content/docs/adr/index.md): chronological architecture decisions.
- [SearchDialog.astro](src/components/SearchDialog.astro): search over the Pagefind index.
- [MermaidInteractive.astro](src/components/MermaidInteractive.astro) and `MermaidStyles.astro`:
  diagram interaction and appearance; inspect these before changing SVG behavior.

## Content/build flow

- Edit Markdown with `title` frontmatter; the content loader validates the schema at build.
- A new page must be listed in `src/lib/spaces.ts` or the build fails. Numbered files under `adr/` join on their own.
- Localized content lives under locale directories in the docs collection (for example `fr/`);
  shared docs catalogs supply chrome copy, rather than translating page bodies automatically.
- Asset references under `src/assets/` are processed by Astro; public assets are served as static files.
- Configured production origin drives canonical URLs, social URL metadata and sitemap generation.
- Shared chrome/styles are consumed through the web-installed `@bagel/ui` link.
- Mermaid code fences become diagrams; pan/zoom interaction is a docs enhancement over rendered output.
- `bun run build` runs Pagefind over `dist`; only translated pages are indexed, so fallback copies do not fill results.

## Invariants and pitfalls

- Keep stable slugs and anchors when editing references; other docs and external links may depend on them.
- Validate service statements against app entry points/handlers, not just another document.
- Do not invent current infrastructure availability from a past QA report.
- Mermaid `autoTheme` stays disabled: its theme replacement would discard custom palette variables.
- The site is dark only, like marketing; `data-surface="tonal"` makes `--bb-border` transparent, so hairlines
  that must show use an explicit colour.
- Pagefind needs `'wasm-unsafe-eval'` in the `public/_headers` CSP. `astro preview` does not apply that file,
  so test search under the real header.
- Use app-level style overrides rather than copying shared primitive markup/CSS into documentation.
- The ADR helper relies on external `adr-tools` and uses macOS `sed -i ''`; it is not a bundled compiler.

## Focused commands

From `web/docs`: `bun run dev`, `bun run build`, `bun run preview`.
From `web`: `bun test docs` covers the page-to-space mapping and catalog keys; build validates collection/rendering changes.
From `web/docs`: `bun run adr:new -- "Decision title"` invokes [bin/adr](bin/adr),
using `.adr/template.md` and the existing local ADR configuration; requires `adr` on PATH.
When shared UI changed, also use its focused tests/build consumers instead of assuming a docs build
checks every adapter contract. Verify diagram/navigation layout visually after relevant changes.
