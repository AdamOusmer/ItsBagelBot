<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# ItsBagelBot marketing site

Static Astro site for [itsbagelbot.com](https://itsbagelbot.com): landing page,
pricing, guides, changelog, command builder, and legal pages. English is served
unprefixed; French, Spanish, Portuguese (Brazil), German and Russian use `/fr/`,
`/es/`, `/pt-br/`, `/de/` and `/ru/` routes. Locales are discovered from
`locales/<code>/website` at the repository root.

## Structure

```text
web/
├── public/          # favicons, logos, robots.txt, _headers (all brand assets are ours)
├── src/
│   ├── assets/      # build-time assets
│   ├── components/  # Astro components
│   ├── content/     # legal pages, changelog JSON collection, guides/ content
│   ├── i18n/        # catalog loader; strings live in /locales/<code>/website
│   ├── layouts/     # Layout.astro (head, CSP, icons, LOCALIZED sets)
│   ├── lib/         # guides/ content model, slug list and parity check, variables reference
│   ├── pages/       # [...lang]/ routes: index, pricing, guides, changelog, builder, legal; vs/
│   ├── script/      # client scripts
│   └── styles/      # global styles + guide-screen.css (mock frame + annotations)
└── tests/           # Playwright tests (site, locale layout, version comparison)
```

## Commands

| Command           | Action                                    |
| :---------------- | :---------------------------------------- |
| `bun install`     | Install dependencies                      |
| `bun run dev`     | Dev server at `localhost:4321`            |
| `bun run build`   | Production build to `./dist/`             |
| `bun run preview` | Preview the production build locally      |
| `bun run test:e2e` | Playwright tests against `bun astro preview` on port 4399 (build first) |

## Cloudflare Pages

This site deploys as the `itsbagelbot` Pages project. Production and
preview both pin `BUN_VERSION=1.4.2` and `SKIP_DEPENDENCY_INSTALL=true`,
because Pages v3 still defaults to Bun 1.2.15 and will `npm install` unless
told not to.

| Setting | Value |
| :------ | :---- |
| Root directory | `web/marketing` |
| Build command | `bun --version && bun install --cwd .. --frozen-lockfile && bun run build` |
| Build output directory | `dist` |

## Guides

The guide pages under `/guides` are content, not markup, and a guide is two
things kept apart:

- **Structure and English copy**, once, in `src/content/guides/<slug>.en.ts`: a
  `GuideContent` (`src/lib/guides/types.ts`) holding the `meta`, the section ids
  and order, the block kinds (`prose`, `callout`, `table`, `chat`, `dash`,
  `steps`, `cards`, `widget`), which mock screen or widget each block shows, and
  the data those widgets take.
- **Translations**, once per language, in `src/content/guides/<slug>.<lang>.json`:
  a flat map from string ids to text, and nothing else. An id is the path to a
  string in the English guide, such as `anatomy.b1.caption` (section id, block
  index, field) or `meta.title`.

`getGuide()` in `src/lib/guides/registry.ts` hydrates the two together:
`translate()` overwrites the English strings with the ids a language supplies,
so an id a language omits renders the English line rather than a blank.
Rendering lives in `src/components/guides/`, routing in
`src/pages/[...lang]/guides/`. The hub page copy comes from `hub.<lang>.json`.

To add a guide:

1. Write `src/content/guides/<slug>.en.ts`.
2. Add the slug to `guideSlugs` in `src/lib/guides/slugs.ts`. Its position sets
   the hub card order, the chapter number, and the prev/next pager. The page
   route, the hub card and the hreflang pairing follow from that one line.

To add a language, copy an existing `<slug>.<lang>.json` and translate the
values. `assertGuideParity()` in `src/lib/guides/parity.ts` runs at module load
and fails `bun run build` and `bun run dev` on a translation id that the English
guide does not name (a typo, or a line left behind by a deleted block), on an
English id the translation lacks, on a file that is not a flat string map, and on
a missing English hub. A separate guard, `src/lib/contentGuards.ts`, fails the
build on an em dash in any content or website locale file.

Two conventions inside a section:

- **Screens** (`src/components/guides/screens/`) are the dashboard mocks a
  `dash` block renders. One component per distinct screen, built from the real
  `@bagel/ui` elements the console renders, so a screen cannot describe a page
  the console no longer has; `src/styles/guide-screen.css` holds only the
  annotation anchor and the overrides that fit console chrome inside a figure.
  Every visible string is a `labels` key with the English text as the default,
  so a locale passes only what differs and never edits the markup.
- **Widgets** (`src/components/guides/widgets/`) are blocks that do something in
  the browser: `Checklist` (first-hour tasks kept in `localStorage`),
  `Rehearsal` (expands the response tokens as you type), `PathPicker`,
  `FetchBudget` and `FetchOutcomes` (data sources), `ModuleCatalog` (filterable
  module grid) and `CounterPlay` (replays `!counter`). Same `labels` rule. Client
  code is an Astro `<script>` (the CSP has no inline allowance) that binds through
  `src/script/bind.ts` on `astro:page-load` behind a `data-ready` guard, so the page transition router
  re-wires a widget after a navigation without binding it twice.

Screens and widgets register by file name: the basename must equal a member of
`ScreenName` or `WidgetName` in `src/lib/guides/types.ts`, and the dispatch maps
in `screens/index.ts` and `widgets/index.ts` glob the directory. A name with no
file fails the build in `GuideBody.astro`.

Glyphs go through `@bagel/ui/astro/Icon.astro` (Lucide, generated into
`ui/lib/icons.ts`); the mocks draw no text arrows or ticks. The "Guide 0N"
eyebrow is computed from the slug order, so content files carry only the word.
