<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# ItsBagelBot Documentation

This directory contains the documentation for ItsBagelBot, a static [Astro](https://astro.build) site that shares its navigation, footer and design tokens with the marketing site through `@bagel/ui`.

## Project Structure

- `src/content/docs/`: Markdown pages.
  - `fr/`: French counterparts of the English documentation pages.
  - `adr/`: Architecture Decision Records.
  - `architecture/`: General architecture documentation.
  - `infrastructure/`: Infrastructure setup and deployment docs.
  - `data-and-state/`: Data models and state management.
  - `guides/`: Contributor and operator onboarding guides.
  - `microservices/`: Details of individual microservices.
  - `qa/`: QA Reports and testing docs.
  - `reference/`: API and system references.
- `src/lib/spaces.ts`: The five top-level sections and which page sits in each.
- `src/layouts/Base.astro`: The page frame (head, shared nav, shared footer, search).
- `src/pages/`: The home page and the route that renders every Markdown page.
- `src/styles/`: The page grid (`docs.css`) and the rendered Markdown look (`prose.css`).
- `public/`: Static assets like favicons.

## Adding a page

Add the Markdown file, then list its id in `src/lib/spaces.ts`. A page in no section fails the build. ADRs are the exception: numbered files under `adr/` join the Records section on their own.

## Cloudflare Pages

This site deploys as the `itsbagelbotdocs` Pages project. Production and
preview both pin `BUN_VERSION=1.4.2` and `SKIP_DEPENDENCY_INSTALL=true`,
because Pages v3 still defaults to Bun 1.2.15 and will `npm install` unless
told not to.

| Setting | Value |
| :------ | :---- |
| Root directory | `web/docs` |
| Build command | `bun --version && bun install --cwd .. --frozen-lockfile && bun run build` |
| Build output directory | `dist` |

## Commands

Run these from the `docs/` directory using `bun`:

| Command                   | Action                                           |
| :------------------------ | :----------------------------------------------- |
| `bun install`             | Installs dependencies                            |
| `bun run dev`             | Starts local dev server at `localhost:4321`      |
| `bun run build`           | Builds the site and search index to `./dist/`    |
| `bun run preview`         | Preview your build locally, before deploying     |
| `bun astro ...`           | Run CLI commands like `astro add`, `astro check` |

Search needs the index from `bun run build`, so it returns nothing under `bun run dev`.

## Architecture Decision Records (ADRs)

ADRs are managed with [`adr-tools`](https://github.com/npryce/adr-tools) (install with `brew install adr-tools`) and live under `src/content/docs/adr/`.

Use the project wrapper so the local template is picked up:

```sh
./bin/adr new "Short title of the decision"
./bin/adr new -s 3 "Supersede decision 3"
./bin/adr list
```

## Writing Documentation

- Pages are standard Markdown with `title` and optional `description` frontmatter.
- We have integrated `astro-mermaid` for diagrams. See `astro.config.mjs` for custom theme configuration.

## Localized documentation

English is at the root and each locale folder with a `docs/` directory under
`locales/` gets a prefixed route (currently `fr`, `es`, `pt-br`, `de` and `ru`).
Page bodies are translated only for French today; other locales get translated
site chrome and English page content. Translated pages mirror the English content
path below `src/content/docs/fr/`; for example, `src/content/docs/guides/getting-started.md`
is translated at `src/content/docs/fr/guides/getting-started.md`. A missing French
page keeps the English documentation available, with a notice, so contributors can
translate one page at a time without creating broken navigation. Keep technical
names, code, links, and diagram identifiers unchanged unless the translated page
needs a localized explanation around them.

Shared navigation, section, metadata, and diagram-control labels live in
`locales/<code>/docs/` at the repository root. Locale configuration is discovered
from the locale folders that contain a `docs/` directory. Run
`python3 scripts/translations.py check --strict fr` from the repository root to
verify French catalog and page coverage. Read
[the contribution guide](../../TRANSLATIONS.md) for editing steps and new languages.
