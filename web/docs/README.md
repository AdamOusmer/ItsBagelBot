<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# ItsBagelBot Documentation

This directory contains the documentation for ItsBagelBot, built with [Astro Starlight](https://starlight.astro.build).

## 🚀 Project Structure

- `src/content/docs/`: Markdown and MDX files for the documentation routes.
  - `fr/`: French counterparts of the English documentation pages.
  - `adr/`: Architecture Decision Records.
  - `architecture/`: General architecture documentation.
  - `infrastructure/`: Infrastructure setup and deployment docs.
  - `data-and-state/`: Data models and state management.
  - `microservices/`: Details of individual microservices.
  - `qa/`: QA Reports and testing docs.
  - `reference/`: API and system references.
- `src/assets/`: Images and other assets used in docs.
- `public/`: Static assets like favicons.
- `astro.config.mjs`: Starlight configuration (sidebar, theme, etc.).

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

## 🧞 Commands

Run these from the `docs/` directory using `bun`:

| Command                   | Action                                           |
| :------------------------ | :----------------------------------------------- |
| `bun install`             | Installs dependencies                            |
| `bun dev`                 | Starts local dev server at `localhost:4321`      |
| `bun build`               | Build your production site to `./dist/`          |
| `bun preview`             | Preview your build locally, before deploying     |
| `bun astro ...`           | Run CLI commands like `astro add`, `astro check` |

## 📝 Architecture Decision Records (ADRs)

ADRs are managed with [`adr-tools`](https://github.com/npryce/adr-tools) (install with `brew install adr-tools`) and live under `src/content/docs/adr/`.

Use the project wrapper so the local template is picked up:

```sh
./bin/adr new "Short title of the decision"
./bin/adr new -s 3 "Supersede decision 3"
./bin/adr list
```

## 👀 Writing Documentation

- The documentation uses Starlight's standard Markdown and MDX capabilities.
- We have integrated `astro-mermaid` for diagrams. See `astro.config.mjs` for custom theme configuration.

## Localized documentation

Starlight is configured with English at the root and French under `/fr/`.
Translated pages mirror the English content path below `src/content/docs/fr/`;
for example, `src/content/docs/guides/getting-started.md` is translated at
`src/content/docs/fr/guides/getting-started.md`. A missing French page keeps
the English documentation available, so contributors can translate one page at
a time without creating broken navigation. Keep technical names, code, links,
and diagram identifiers unchanged unless the translated page needs a localized
explanation around them.

Shared navigation, sidebar, metadata, and diagram-control labels live in
`src/i18n/locales/<code>.json`. Locale configuration is discovered from these
catalogs. Run `python3 scripts/translations.py check --strict fr` from the
repository root to verify French catalog and page coverage. Read
[the contribution guide](../../TRANSLATIONS.md) for editing steps and new languages.
