<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# ItsBagelBot marketing site

Static Astro site for [itsbagelbot.com](https://itsbagelbot.com): landing page,
pricing, guides, changelog, command builder, and legal pages, in English and French
(`/fr/` routes).

## Structure

```text
web/
├── public/          # favicons, logos, robots.txt, _headers (all brand assets are ours)
├── src/
│   ├── assets/      # build-time assets
│   ├── components/  # Astro components
│   ├── content/     # legal pages, changelog JSON collection, guides/ copy
│   ├── i18n/        # EN/FR catalogs
│   ├── layouts/     # Layout.astro (head, CSP, icons, LOCALIZED sets)
│   ├── lib/         # guides/ content model, slug list and parity check
│   ├── pages/       # [...lang]/ routes: index, pricing, guides, changelog, builder, legal
│   ├── script/      # client scripts
│   └── styles/      # global styles + dashframe.css (the df-* mock library)
└── tests/           # Playwright tests
```

## Commands

| Command           | Action                                    |
| :---------------- | :---------------------------------------- |
| `bun install`     | Install dependencies                      |
| `bun run dev`     | Dev server at `localhost:4321`            |
| `bun run build`   | Production build to `./dist/`             |
| `bun run preview` | Preview the production build locally      |

## Cloudflare Pages

This site deploys as a static Cloudflare Pages project:

- Build command: `bun run build`
- Build output directory: `dist`

## Guides

The guide pages under `/guides` are content, not markup. Copy lives in
`src/content/guides/<slug>.<lang>.ts`, one file per guide per language, each
default-exporting a `GuideContent` typed by `src/lib/guides/types.ts`. A guide
is a list of sections, and a section is a list of blocks: `prose`, `callout`,
`table`, `chat`, `dash`, `steps`, `cards`, `widget`. Rendering lives in
`src/components/guides/`, routing in `src/pages/[...lang]/guides/`.

To add a guide:

1. Write `src/content/guides/<slug>.en.ts` and `<slug>.fr.ts`.
2. Add the slug to `guideSlugs` in `src/lib/guides/slugs.ts`. Its position sets
   the hub card order, the chapter number, and the prev/next pager. The page
   route, the hub card and the hreflang pairing follow from that one line.

French is not optional. Every English change needs the French twin in the same
commit, because `assertGuideParity()` in `src/lib/guides/registry.ts` runs at
module load and fails `bun run build` and `bun run dev` when the two drift: a
missing file, different section ids or order, a different block sequence, a
table with a different shape, a different number of chat lines or dashboard
notes, or an em dash anywhere in either language.

Two conventions inside a section:

- **Screens** (`src/components/guides/screens/`) are the hand-built dashboard
  mocks a `dash` block renders. One component per distinct screen, built from
  the `df-*` classes in `src/styles/dashframe.css`. Every visible string is a
  `labels` key with the English text as the default, so a locale passes only
  what differs and never edits the markup.
- **Widgets** (`src/components/guides/widgets/`) are blocks that do something in
  the browser: `Checklist` (first-hour tasks kept in `localStorage`),
  `Rehearsal` (expands the response tokens as you type), `PathPicker`,
  `FetchBudget` and `FetchOutcomes` (data sources), `ModuleCatalog` (filterable
  module grid) and `CounterPlay` (replays `!counter`). Same `labels` rule. Client
  code is an Astro `<script>` (the CSP has no inline allowance) that binds on
  `astro:page-load` behind a `data-ready` guard, so the page transition router
  re-wires a widget after a navigation without binding it twice.

Screens and widgets register by file name: the basename must equal a member of
`ScreenName` or `WidgetName` in `src/lib/guides/types.ts`, and the dispatch maps
in `screens/index.ts` and `widgets/index.ts` glob the directory. A name with no
file fails the build in `GuideBody.astro`.

Glyphs go through `src/components/ui/Icon.astro` (Lucide, generated into
`src/lib/icons.ts`); the mocks draw no text arrows or ticks. The "Guide 0N"
eyebrow is computed from the slug order, so content files carry only the word.
