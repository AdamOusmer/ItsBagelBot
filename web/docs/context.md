# Documentation site context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md).
This is the Astro/Starlight documentation package, deployed as a static site.

## Responsibility and boundaries

- Publishes architecture, infrastructure, service contracts, operating guides and decision history.
- Canonical production origin is configured as `https://docs.itsbagelbot.com`.
- Markdown/MDX content is rendered at build time; there is no service database or authenticated console.
- Runtime behavior belongs to the relevant app/service code; documentation must be checked against it.
- Broadcaster tutorial/marketing content primarily belongs to [marketing](../marketing/context.md).
- Design contracts belong to [ui](../../ui/context.md); Starlight-specific adaptation stays here.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Docs collection | Starlight content collection loaded from `src/content/docs/`. |
| Reference | Current shape, contracts and inventories used while implementing changes. |
| Architecture | System boundaries, ownership and communication model. |
| Data & State | Persistence/projection/cache model across services. |
| ADR | Numbered record explaining a consequential architectural decision. |
| QA report | Evidence from a particular review or validation run; not a promise of current correctness. |
| Starlight wrapper | Local adapter satisfying Starlight props/slots while composing shared visual UI. |
| Mermaid diagram | Code-defined architecture illustration rendered through the Mermaid integration. |

Root glossary terms apply to product concepts; keep code naming distinct from user-facing language.
An old ADR describes the decision at the time; it may have been superseded by later code/decisions.

## Code and content navigation

- [astro.config.mjs](astro.config.mjs): production origin, Starlight sidebar, component overrides,
  Mermaid palette/layout, integration configuration and strict Mermaid security mode.
  UI locale labels/sidebar translations load from root `locales/<lang>/docs/`; English is `root`.
- [src/content.config.ts](src/content.config.ts): `docsLoader` plus Starlight `docsSchema`.
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
- [src/styles/theme.css](src/styles/theme.css): Starlight `--sl-*` remapping and docs visual overrides.
- [Header.astro](src/components/Header.astro) and [Footer.astro](src/components/Footer.astro):
  preserve Starlight's slots/props while composing shared UI adapters.
- `Head.astro`, `SkipLink.astro`, `SidebarState.astro`, `SmoothScroll.astro`:
  docs head/accessibility/sidebar and navigation-specific behavior.
- [MermaidInteractive.astro](src/components/MermaidInteractive.astro) and `MermaidStyles.astro`:
  diagram interaction and appearance; inspect these before changing SVG behavior.

## Content/build flow

- Edit Markdown/MDX with required Starlight frontmatter; content loader validates schema at build.
- Sidebar sections autogenerate from named directories, so folder placement affects navigation.
- Localized content lives under locale directories in the docs collection (for example `fr/`);
  shared docs catalogs supply chrome/sidebar copy, rather than translating page bodies automatically.
- Asset references under `src/assets/` are processed by Astro; public assets are served as static files.
- Configured production origin drives canonical URLs, social URL metadata and sitemap generation.
- Shared chrome/styles are consumed through the web-installed `@bagel/ui` link.
- Mermaid code fences become diagrams; pan/zoom interaction is a docs enhancement over rendered output.

## Invariants and pitfalls

- Keep stable slugs and anchors when editing references; other docs and external links may depend on them.
- Validate service statements against app entry points/handlers, not just another document.
- Do not invent current infrastructure availability from a past QA report.
- Mermaid `autoTheme` stays disabled: its theme replacement would discard custom palette variables.
- Light/dark diagram adaptation lives in CSS; preserve legible text, nodes, labels and clusters.
- Starlight Header/Footer need local wrappers; replacing them directly with raw shared components
  can discard search, theme-toggle or pagination slots.
- Use app-level style overrides rather than copying shared primitive markup/CSS into documentation.
- The ADR helper relies on external `adr-tools` and uses macOS `sed -i ''`; it is not a bundled compiler.

## Focused commands

From `web/docs`: `bun run dev`, `bun run build`, `bun run preview`.
There is no `check` or `test` script in this package; build validates collection/rendering changes.
From `web/docs`: `bun run adr:new -- "Decision title"` invokes [bin/adr](bin/adr),
using `.adr/template.md` and the existing local ADR configuration; requires `adr` on PATH.
When shared UI changed, also use its focused tests/build consumers instead of assuming a docs build
checks every adapter contract. Verify diagram/navigation layout visually after relevant changes.
