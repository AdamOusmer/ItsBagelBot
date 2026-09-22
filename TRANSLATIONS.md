<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Help translate ItsBagelBot

You can contribute a spelling correction, a better sentence, or a new language. Translation-only pull requests are welcome; you do not need to arrange a code contribution first. No programming experience is needed to improve an existing JSON catalog.

## Make your first correction

1. Choose the file below for the screen or message you want to improve. On GitHub, open the file and choose **Edit this file** (the pencil).
2. Find the existing sentence. Change its value, leaving the key, quotes, commas, and placeholders intact.
3. Choose **Propose changes** and open a pull request. Mention the language and where the message appears. A screenshot or example chat reply helps reviewers.
4. GitHub runs the translation checks. If one fails, its output names the file and key to fix. A maintainer can help with the syntax and preview.

For example, translate the value on the right, not `cmd.added` or its placeholders:

```json
"cmd.added": "@{user} la commande {command} a été ajoutée"
```

If a sentence appears on screen but you cannot find it in these files, [report a translation gap](https://github.com/AdamOusmer/ItsBagelBot/issues/new?template=translation.yml). Include the page or command and selected language. It may still be hardcoded; do not hide it by adding an unused key.

## Find the right files

Paths are relative to the repository root. English (`en`) is the reference language.

| What you are translating | Location |
| --- | --- |
| Sesame chat replies and service notifications | `internal/domain/i18n/locales/<code>.json` |
| Dashboard, admin, public commands page, module labels, and reply-editor copy | `web/kit/lib/i18n/locales/<code>.json` |
| Marketing website navigation and shared copy | `web/marketing/src/i18n/locales/<code>.json` |
| Marketing guides | `web/marketing/src/content/guides/` (English guide structure plus `<slug>.<code>.json` string maps) |
| Terms, privacy, creator terms | `web/marketing/src/content/legal/<document>/<code>/` |
| Release notes | `web/marketing/src/content/changelog/*.json` (`title` and `description` locale maps) |
| Documentation navigation, sidebar, and metadata | `web/docs/src/i18n/locales/<code>.json` |
| Technical documentation website | `web/docs/src/content/docs/` (Markdown/MDX; see the docs README for locale layout) |
| Manually sent support emails | `mail/` (HTML and plain-text versions; see its README) |
| Automated Premium, gift, and giveaway emails | `internal/domain/i18n/locales/<code>.json` (`mail.*` keys; renderers in `app/db/transactions/mail/`) |
| Development-only checkout copy | `web/dashboard/src/routes/(app)/billing/demo-checkout/copy/<code>.json` (kept out of production bundles) |
| Registered application languages | `internal/domain/i18n/locales.json` |

The JSON catalogs have different shapes: chat and website catalogs use flat keys; the console uses nested objects and some arrays. Follow the neighboring English file. Do not edit generated `web/kit/lib/i18n/keys.d.ts` by hand.

Broadcaster-authored command replies and saved custom templates are their content. Changing the interface language must not rewrite those messages. Translate the built-in defaults and editor hints instead.

## Check your changes locally (optional)

From the repository root, with Python 3 installed and no other dependencies:

```sh
python3 scripts/translations.py check
```

This checks all four UI/chat catalogs for valid JSON, duplicate keys, invalid values, empty translations, unknown keys, and damaged placeholders. It also prints coverage by language and surface.

```sh
python3 scripts/translations.py check --missing
python3 scripts/translations.py check --strict fr
```

`--missing` lists work still to translate. `--strict fr` requires full French catalog, documentation-page, and legal-file coverage, as CI does. New languages may be partial and fall back to English. These counts measure catalog entries, not linguistic quality or all website prose; file presence does not establish a faithful translation. Legal pages, guides, documentation, and static email files need their own review and preview.

Developers should also run the checks for the surface they change:

```sh
# Chat replies and notification catalogs
 go test ./internal/domain/i18n ./app/twitch/sesame/modules ./app/twitch/sesame/engine
# Console catalog shape and generated English key types
 node web/kit/scripts/check-i18n.mjs
 node web/kit/scripts/gen-i18n-keys.mjs
# After installing the locked web dependencies
 cd web
 bun run check
```

Keep English and French complete when introducing new product copy. A translation-only change to French does not require regenerating the English key types. Review template formatting, links, plural forms, accessibility labels, error states, and narrow screens in the affected language.

## Start another language

Use a lowercase locale such as `es` or `pt-br` (maximum eight characters). The helper creates partial chat, console, website, and docs-UI catalogs and registers the locale without copying English sentences that might look like completed translations:

```sh
python3 scripts/translations.py new es --name 'Español' --og-locale es_ES
```

It refuses to overwrite an existing locale. Add translated keys from the neighboring English files to the new catalogs. Start small; absent keys use English. The helper covers the application catalogs, not the separate guide, legal, documentation, or mail content.

Without Python, create the same four `<code>.json` files manually and add the code to `internal/domain/i18n/locales.json`. Include `lang.name` in the website catalog, nested `lang.name` in the console catalog, and `lang.ogLocale` in the website catalog. The docs UI catalog also needs `lang.name`. A chat catalog may begin as `{}`. Keep the files and registry together so the frontend does not offer a language the backend rejects.

Ask a maintainer to review locale selection, route generation, and fallback before enabling the new language in production. Some surfaces have separate locale configuration; adding an application catalog is not a claim that every document is translated.

## Preserve the parts the software reads

- Translate values only. Keep keys, command names (`!raffle`, `!time`), URLs, HTML attributes, and variable syntax unchanged.
- Keep named placeholders such as `{user}`, `{count}`, `{command}`, and `{dashboard_url}` verbatim. You can move them within a sentence.
- Keep Go formatting placeholders such as `%s`, `%d`, and `%.1f` in the same order and with the same type. `%%` means a literal percent sign.
- Keep template markers such as `[[ ... ]]` and `{{.Field}}` unchanged. Translate the surrounding prose in both HTML and plain-text email variants.
- Keep arrays in the same order and with the same number of entries.
- Preserve intentional empty values; do not replace real text with an empty translation.
- Keep product names and security promises accurate. Do not translate sender domains or weaken anti-phishing instructions.

## Guides, legal pages, and release notes

Marketing guides are data-driven; they are **not** separate Astro pages per language. Translate the plain JSON `<slug>.<code>.json` files; English structure remains in `<slug>.en.ts` and does not need editing. A locale's string map overlays the English structure. Preserve string IDs, block order, anchors, and widget/sample data. Existing guide files must satisfy the guide parity check; a completely absent locale guide falls back to English. The hub has its own `hub.<code>.json` file. Ask a maintainer to generate the initial guide key map when starting a language.

Legal sections are `NN-anchor.md` files. Preserve names and anchors; translate the `heading`, `plain` summary, and body. `meta.json` contains the title, description, and update label. Legal meaning needs maintainer review, not just a passing build.

Release files identify a version with `tag`, `version`, `date`, and `github`; leave those unchanged. Add the locale inside the `title` and `description` maps, keeping `en`. If either field is currently a plain English string, convert it to a map with `en` plus the new locale. Missing locales fall back to English.

## Review expectations

A small correction is enough for a useful contribution. Describe what changed and your familiarity with the language; machine-assisted drafts should be identified so a fluent reviewer can check them. Passing checks cannot judge tone, meaning, or fluency. Maintainers review and merge changes; this guide does not ask contributors to publish or send anything to users.
