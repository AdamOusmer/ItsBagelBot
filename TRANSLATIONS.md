<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Help translate ItsBagelBot

You can contribute a spelling correction, a better sentence, or a whole new language. Translation-only pull requests are welcome; you do not need to arrange a code contribution first. No programming experience is needed to improve an existing translation file.

## The one rule

Everything for a language lives in `locales/<code>/`, one small file per feature, at the same paths as `locales/en/`.

English (`en`) is the reference. To translate `locales/en/console/timers.json` into French, edit `locales/fr/console/timers.json`: same folder, same file name, same keys, your words. `locales/manifest.json` lists the languages the app offers.

## What each folder covers

| Folder | What it translates | Example file |
| --- | --- | --- |
| `locales/<code>/chat/` | Sesame chat replies, service notifications, and the automated Premium, gift, and giveaway emails (`mail.json`) | `locales/fr/chat/loyalty.json` |
| `locales/<code>/console/` | Dashboard, admin, public commands page, module labels, and reply-editor copy | `locales/fr/console/timers.json` |
| `locales/<code>/website/` | Marketing website navigation and shared copy | `locales/fr/website/hero.json` |
| `locales/<code>/docs/` | Documentation site chrome: navigation, sidebar, menus, and metadata | `locales/fr/docs/menu.json` |
| `ui/locales/<code>/` | Shared controls on every site: dialogs, search, dropdowns, save status, deploy steps | `ui/locales/fr/status.json` |

The shared controls are the one exception to the one rule: their files live inside the `ui/` library so it can ship on its own. The same rules apply. When you add a language folder there, run `bun ui/scripts/gen-locales.mjs` so the library loads it; the checks tell you if you forget.

Bigger features are folders. `locales/fr/console/admin/users.json` holds the admin users page, and `locales/fr/console/admin/index.json` holds the text shared by the whole admin area.

The file path is the start of every key inside it. `"added"` in `locales/fr/chat/cmd.json` is the key `cmd.added`, and `"pagerNext"` in `locales/fr/console/admin/users.json` is `admin.users.pagerNext`. The checks use these full keys in their messages. Inside a file, keys may be nested objects (most console files) or dotted names (chat files); follow the English file next to yours.

Broadcaster-authored command replies and saved custom templates are their content. Changing the interface language must not rewrite those messages. Translate the built-in defaults and editor hints instead.

## Fix one sentence

Say the French reply to `!cmd add` should sound more natural.

1. Open `locales/fr/chat/cmd.json` on GitHub and choose **Edit this file** (the pencil). Its English twin is `locales/en/chat/cmd.json`.
2. Find the sentence and change only the text on the right. This line:

   ```json
   "added": "@{user} la commande {command} a été ajoutée",
   ```

   becomes:

   ```json
   "added": "@{user} la commande {command} a bien été ajoutée",
   ```

   Keep the key (`added`), the quotes, the comma at the end of the line, and the placeholders `{user}` and `{command}`.
3. Choose **Propose changes** and open a pull request. Mention the language and where the message appears. A screenshot or example chat reply helps reviewers.
4. GitHub runs the translation checks. If one fails, its output names the file and key to fix. A maintainer can help with the syntax and preview.

If a sentence appears on screen but you cannot find it in `locales/`, [report a translation gap](https://github.com/AdamOusmer/ItsBagelBot/issues/new?template=translation.yml). Include the page or command and selected language. It may still be hardcoded; do not hide it by adding an unused key.

## Add a language

Pick a lowercase code such as `es` or `pt-br` (maximum eight characters), the language's own name, and its Open Graph locale for social cards:

```sh
python3 scripts/translations.py new es "Español" es_ES
```

This creates `locales/es/` with only the few files the app needs to list the language (its name, plus the social-card locale for the website) and registers `es` in `locales/manifest.json`. It refuses to overwrite a language that already exists.

A partial language is fine. Anything you have not translated yet shows in English, so you can start with one file, such as `locales/es/chat/cmd.json`, and grow from there. Copy an English file to the same path under your language, translate the values, and remove lines you are not ready to translate instead of leaving English in place. English left in your files looks like finished work to the checks.

Without Python, create the same starter files by hand: `locales/es/console/lang.json` with `{"name": "Español"}`, `locales/es/website/lang.json` with `{"name": "Español", "ogLocale": "es_ES"}`, and `locales/es/docs/lang.json` with `{"name": "Español"}`. Then add `"es"` to `locales/manifest.json`. Keep the files and the manifest together so the website does not offer a language the bot rejects.

Ask a maintainer to review locale selection, route generation, and fallback before enabling the new language in production. Some surfaces have separate locale configuration, and the [longer documents](#longer-documents) are translated separately; adding a language here is not a claim that every document is translated.

## See what is missing

```sh
python3 scripts/translations.py status es
```

This prints each surface's progress and the files that still need work:

```text
chat: 0/460 keys translated
  locales/es/chat/accountage.json: 2 missing (new file)
  locales/es/chat/activity.json: 8 missing (new file)
  ...
console: 1/4343 keys translated
  locales/es/console/lang.json: 2 missing
  ...
```

`(new file)` means your language does not have that file yet; copy it from `locales/en/` and translate it. Add `--keys` to list every missing key under its file.

## Check your changes locally (optional)

From the repository root, with Python 3 installed and no other dependencies:

```sh
python3 scripts/translations.py check
python3 scripts/translations.py check --missing
python3 scripts/translations.py check --strict fr
```

`check` validates every file under `locales/` and prints coverage by language and surface. `--missing` lists each missing key with its file. `--strict fr` also requires complete French files, documentation pages, and legal files, as CI does. New languages may stay partial and fall back to English. These counts measure entries and files, not linguistic quality; a file being present does not establish a faithful translation.

## What the checks enforce

- **Small files.** A file holds at most 150 keys (a list counts as one). When an English file outgrows that, it becomes a folder: `modules/index.json` keeps the shared keys and files such as `modules/catalog/timers.json` hold each group.
- **Same paths as English.** Every file in your language needs an English file at the same path, and may only contain keys that English file has. If a key sits in the wrong file, the error names the file where it belongs.
- **One home per key.** Each full key is defined by exactly one file. In console files, a key cannot be both a sentence and a group of keys.
- **Plain names.** File and folder names start with a lowercase letter and use only letters, digits, and `_`, like `timers` or `channel_points`. Only `.json` files belong in `locales/`.
- **Flat chat files.** Files in `chat/` are single-level `"key": "text"` pairs kept directly in `chat/`, without subfolders. Write deeper keys with dots, like `"counter.created"`.
- **Valid values.** Each file is a JSON object without duplicate keys. Values are text; console files may also use lists of text.
- **Intact placeholders.** Named placeholders, printf placeholders in chat files, and list lengths must match English. The next section explains them.

## Preserve the parts the software reads

- Translate values only. Keep keys, command names (`!raffle`, `!time`), URLs, HTML attributes, and variable syntax unchanged.
- Keep named placeholders such as `{user}`, `{count}`, `{command}`, and `{dashboard_url}` verbatim. You can move them within a sentence.
- Keep Go formatting placeholders such as `%s`, `%d`, and `%.1f` in the same order and with the same type. `%%` means a literal percent sign.
- Keep template markers such as `[[ ... ]]` and `{{.Field}}` unchanged. Translate the surrounding prose in both HTML and plain-text email variants.
- Keep arrays in the same order and with the same number of entries.
- Preserve intentional empty values; do not replace real text with an empty translation.
- Keep product names and security promises accurate. Do not translate sender domains or weaken anti-phishing instructions.

## Longer documents

Long-form content keeps its own layout outside `locales/`:

| What you are translating | Location |
| --- | --- |
| Marketing guides | `web/marketing/src/content/guides/` (English guide structure plus `<slug>.<code>.json` string maps) |
| Terms, privacy, creator terms | `web/marketing/src/content/legal/<document>/<code>/` |
| Release notes | `web/marketing/src/content/changelog/*.json` (`title` and `description` locale maps) |
| Technical documentation pages | `web/docs/src/content/docs/` (Markdown/MDX; see the docs README for locale layout) |
| Manually sent support emails | `mail/` (HTML and plain-text versions; see its README) |
| Development-only checkout copy | `web/dashboard/src/routes/(app)/billing/demo-checkout/copy/<code>.json` (kept out of production bundles) |

Marketing guides are data-driven; they are **not** separate Astro pages per language. Translate the plain JSON `<slug>.<code>.json` files; English structure remains in `<slug>.en.ts` and does not need editing. A locale's string map overlays the English structure. Preserve string IDs, block order, anchors, and widget/sample data. Existing guide files must satisfy the guide parity check; a completely absent locale guide falls back to English. The hub has its own `hub.<code>.json` file. Ask a maintainer to generate the initial guide key map when starting a language.

Legal sections are `NN-anchor.md` files. Preserve names and anchors; translate the `heading`, `plain` summary, and body. `meta.json` contains the title, description, and update label. Legal meaning needs maintainer review, not just a passing build.

Release files identify a version with `tag`, `version`, `date`, and `github`; leave those unchanged. Add the locale inside the `title` and `description` maps, keeping `en`. If either field is currently a plain English string, convert it to a map with `en` plus the new locale. Missing locales fall back to English.

## For developers

Keep English and French complete when introducing new product copy. Add each string to the English file for its feature and the matching French file, and split a file into a folder before it passes 150 keys. A translation-only change to French does not require regenerating the English key types.

Run the checks for the surface you change:

```sh
# Translation files, all surfaces
python3 scripts/translations.py check --strict fr
# Chat replies and notification catalogs
go test ./internal/domain/i18n ./app/twitch/sesame/modules ./app/twitch/sesame/engine
# Console catalog shape and generated English key types
bun web/kit/scripts/check-i18n.mjs
bun web/kit/scripts/gen-i18n-keys.mjs
# After installing the locked web dependencies
cd web
bun run check
```

Do not edit the generated `web/kit/lib/i18n/keys.d.ts` by hand. Review template formatting, links, plural forms, accessibility labels, error states, and narrow screens in the affected language.

## Review expectations

A small correction is enough for a useful contribution. Describe what changed and your familiarity with the language; machine-assisted drafts should be identified so a fluent reviewer can check them. Passing checks cannot judge tone, meaning, or fluency. Maintainers review and merge changes; this guide does not ask contributors to publish or send anything to users.
