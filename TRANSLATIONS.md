# Translating ItsBagelBot

Every user-facing string lives in plain data files: JSON for interface text and chat replies, Markdown for the legal pages. Adding or improving a language never requires touching code, and nothing in a translation file is ever executed. You edit data, open a pull request, and CI rebuilds everything.

## Where translations live

| Surface | Files | Format |
| --- | --- | --- |
| Master locale list (backend validation) | `internal/domain/i18n/locales.json` | JSON array of codes, e.g. `["en", "fr"]` |
| Chat replies + notification copy (Go services) | `internal/domain/i18n/locales/<code>.json` | Flat JSON: `"key": "template"` |
| Dashboard + admin console | `console/shared/lib/i18n/locales/<code>.json` | Nested JSON tree, leaves are strings or arrays of strings |
| Marketing site | `web/src/i18n/locales/<code>.json` | Flat JSON with dotted keys |
| Legal pages (terms, privacy, creator terms) | `web/src/content/legal/<doc>/<code>/` | `meta.json` + one Markdown file per section |
| Guides (`/guides/*`) | not yet data-driven | Still duplicated `.astro` pages per locale; ask a developer |

English (`en`) is the source of truth everywhere. A key missing from your language falls back to English at runtime, so a partial translation is safe to ship: builds never fail on missing translations, only on invalid files.

## Adding a language (example: Spanish, code `es`)

1. Add `"es"` to `internal/domain/i18n/locales.json`.
2. Copy each `en` file to `es` and translate the values (never the keys):
   - `internal/domain/i18n/locales/en.json` → `es.json`
   - `console/shared/lib/i18n/locales/en.json` → `es.json`
   - `web/src/i18n/locales/en.json` → `es.json`
   - `web/src/content/legal/terms/en/` → `terms/es/` (same for `privacy` and `creator-terms`)
3. Open a pull request.

That is the whole process. The dashboard language switcher, the site's `/es/` routes, hreflang tags, and backend validation all pick the new locale up automatically. You can start with just the console and site files; anything you have not translated yet shows English.

Locale codes are lowercase base tags (`es`, `pt-br`), maximum 8 characters.

## Rules for JSON files

- Files are UTF-8 JSON. JSON allows no comments and requires exact syntax (a linter or editor with JSON support helps; a malformed file fails the build with the file name).
- Never rename, add, or delete keys. Translate values only. Extra keys are reported as warnings.
- Placeholders must survive verbatim:
  - `{user}`, `{count}`, `{command}` style tokens are substituted at runtime. Keep them exactly as written; you may reorder them in the sentence.
  - `%s`, `%d` in chat replies are positional (Go format verbs): their order matters, do not reorder them.
  - `{dashboard_url}` in the Go catalog is replaced with the dashboard link at startup. Keep it.
- Arrays are ordered lists (feature bullets, headline words): translate each entry, keep the count and order.
- `lang.name` is the name of your language in itself ("Español", not "Spanish"). It labels the language switcher.
- `lang.ogLocale` (site catalog) is the social-card locale code in `xx_XX` form, e.g. `es_ES`.

## Rules for legal Markdown

- Each section file is `NN-anchor.md`. Keep the filename exactly: the number is the order and the rest is the page anchor that deep links point at.
- Translate the frontmatter `heading` and `plain` (the plain-words summary) and the body below the `---` line.
- Basic Markdown only: paragraphs, bold, links, lists. The few inline HTML links (like the Tebex link) can stay as they are.
- `meta.json` holds the page title, description, and the "updated" label.

## What happens when something is missing or broken

- Missing key or file: English shows in its place. The build logs and service startup logs list every gap per locale (warnings, never failures).
- Invalid JSON, a non-string value, or a missing `en` file: the build or service startup fails immediately and names the file. That protects production from a corrupt catalog, not from an incomplete translation.

## Submitting

Open a pull request with your files. CI rebuilds the Go services, both console apps, and the site; Cloudflare Pages produces a preview of the marketing site. Nothing goes live until the PR is merged.
