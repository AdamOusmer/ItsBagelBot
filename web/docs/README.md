<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Engineering documentation

This Astro site explains the code: service ownership, event paths, state, contracts and architecture decisions. Product setup guides live on the marketing site.

## Work locally

From the repository root:

```sh
cd web
bun install --frozen-lockfile
cd docs
bun run dev
```

Use the Bun version declared in `web/package.json`. The workspace postinstall links `@bagel/ui` from `ui/` and installs its production dependencies.

```sh
# From web/docs
bun test src
bun run check:content
bun run check:diagrams
bun run build
bun run preview
```

The build generates the site and Pagefind search index in `dist/`. Search needs a completed build; the dev server alone does not generate its index.

## Documentation model

The navigation separates **Guides**, **Architecture**, **Services**, **Reference**, and **Decisions**. Published URLs stay stable even when a page changes navigation groups. Task guides state prerequisites and completion checks; references define interfaces; explanations cover architecture and trade-offs; ADRs retain decision history.

Read the [documentation standard](src/content/docs/reference/documentation-standards.md) for the writing rules and review criteria. Reusable starting points live in [templates/](templates/); the ADR generator uses [.adr/template.md](.adr/template.md).

## Content layout

| Path | Owns |
| --- | --- |
| `src/content/docs/guides/` | Setup and testing |
| `src/content/docs/architecture/` | System boundaries and event paths |
| `src/content/docs/microservices/` | Service implementation guides |
| `src/content/docs/data-and-state/` | Persistence, projections and caching |
| `src/content/docs/infrastructure/` | Runtime configuration, observability, performance and release code |
| `src/content/docs/reference/` | Contracts, shared packages and extension points |
| `src/content/docs/adr/` | Sequential architecture decision records |
| `src/content/docs/qa/` | Evidence conventions and validation notes |
| `src/content/docs/fr/` | Matching French pages |

`src/lib/spaces.ts` assigns pages to navigation groups. Every English page must appear exactly once. Numbered ADRs are discovered through the `adr/` prefix. `src/pages/[...slug].astro` renders the content; `src/layouts/Base.astro` supplies the shared frame.

## Write a page

Use Markdown with required `title`, `description`, and `docType` frontmatter. Choose `tutorial`, `how-to`, `reference`, `explanation`, `decision`, or `record` according to the page's purpose. The Astro schema and content policy validate the metadata. Quote YAML values containing colons. Explain ownership, the normal path, failure behavior and the code to read next. Link implementation sources and distinguish implemented behavior from a proposal. Use Mermaid for a path that is easier to follow as a diagram; keep node labels short and quote punctuation.

Add the page ID to navigation and add the French page at the same relative path under `fr/`. Internal site links use routes such as `/microservices/sesame/`; French pages use `/fr/microservices/sesame/`. Source links point to repository files rather than local filesystem paths.

When moving a page, add a redirect in `astro.config.mjs`. Keep old decision URLs resolvable after renumbering. Run the content check, docs tests, site build and French coverage check before finishing:

```sh
# From the repository root
python3 scripts/translations.py check --strict fr
```

The content check parses YAML and Markdown, validates page types and heading structure, and checks navigation, translation parity, local routes and anchors, source targets, image targets, and ADR numbering. The diagram check renders every Mermaid block in Chromium; run `bunx playwright install chromium` once to install the browser. The site build checks that Astro can render the result.

## Continuous validation

[The documentation workflow](../../.github/workflows/docs.yml) runs policy tests, content checks, French coverage, browser diagram rendering, and the production build on pull requests and main pushes. It also runs for source changes because a rename can break a documentation source link. These checks support source review; they cannot prove a claim or translation correct.

## Decisions

```sh
# From web/docs
bun run adr:new "Short title"
```

The generator needs Python 3 and creates the next numbered English record with frontmatter. Complete its context, decision and trade-off, then add the French counterpart. Keep original dates when editing an existing record; describe a later change as a supersession rather than inventing a new history.

## Localization

English is canonical. French mirrors page bodies; other configured locales use English fallback with translated chrome. Locale catalogs are discovered from `locales/*/docs/`. See [TRANSLATIONS.md](../../TRANSLATIONS.md) for the catalog workflow.
