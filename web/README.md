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
│   ├── content/     # legal pages + changelog JSON collection
│   ├── i18n/        # EN/FR catalogs
│   ├── layouts/     # Layout.astro (head, CSP, icons, LOCALIZED sets)
│   ├── pages/       # index, pricing, guides, changelog, command-builder, legal, fr/
│   ├── script/      # client scripts
│   └── styles/      # global styles
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
