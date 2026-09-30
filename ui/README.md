# @bagel/ui

The ItsBagelBot design library: CSS contracts and framework-free browser
engines, plus a Svelte adapter and an Astro adapter that render the same
markup.

Standalone on purpose. It sits at the repo root rather than inside the `web/`
workspace so it has its own `package.json`, its own `bun.lock` and its own CI
job, and so it can be lifted into its own repository unchanged the day the bot
is split up. Nothing in here knows anything about the bot.

## Layout

```
fonts/     self-hosted latin woff2
styles/    layers.css, brand.css, card-atmosphere.css, elements/*.css
lib/       framework-free browser engines (cursor-engine, light-field, reveal, …)
svelte/    Svelte adapters, plus `use:` actions over the lib/ engines
astro/     Astro adapters
scripts/   assert-framework-free.mjs, size.ts
test/      parity.test.ts
```

`lib/` and `styles/` are the domain half: no `svelte`, no `$app/*`, no
`astro:*`, no `node:*` builtin. `svelte/` and `astro/` are adapters and exist
precisely to import their framework. `ui/scripts/assert-framework-free.mjs`
fails the check if that is violated, or if anything in the package imports
`@bagel/kit` — the dependency direction is `ui -> nothing internal`,
`kit -> ui`.

## Consuming it

```js
import { field } from '@bagel/ui/lib/light-field';
import '@bagel/ui/styles/brand.css';
import syne from '@bagel/ui/fonts/syne-latin.woff2?url';
```

The five `web/` packages reach it through a symlink at
`web/node_modules/@bagel/ui`, created by `web/package.json`'s `postinstall`
rather than declared as a dependency. That is not the shape anyone would pick
first; it is what bun 1.4.2 leaves once `link:../ui` (fails to link, exits 1),
`file:../../ui` (hardlinks a snapshot, so edits here do not reach the console)
and an out-of-root `workspaces` entry (silently ignored) are ruled out. The
measurements are in `web/README.md` under "postinstall links the design
library"; that is also the file to update if a later bun makes one of them work.

### The exports map is the public surface

`package.json` `exports` lists what apps may import: components by extension
(`./svelte/*.svelte`, `./astro/*.astro`), stylesheets (`./styles/*.css`), fonts
(`./fonts/*.woff2`), the two barrels, the stateful svelte modules (`toast`,
`inspector`, `discard-guard`, `actions`, `forms`, `i18n`) and each `lib` module
an app imports, one entry per file. Anything else (tests, scripts, fixtures,
internal helpers) is unreachable through the package name. A new `lib` module
becomes public only by adding its line.

Apps import components and modules directly. The barrels (`./svelte`,
`./astro`) exist for external consumers; `web/kit/scripts/ui-barrel-not-imported.test.ts`
fails if anything under `web/` imports them.

`./lib/<name>` maps to `./lib/<name>.ts` so consumers write
`@bagel/ui/lib/light-field` without the extension.

### The symlink means ui's own dependencies must be installed

Vite resolves imports through that symlink from the **realpath**, so an import
that reaches `ui/lib/lenis.ts` looks for `lenis` in `ui/node_modules` and never
in `web/node_modules`. If `ui/` has never been installed, the symptom is a build
or dev-server error reading:

```
Cannot find module 'lenis'
```

`web/package.json`'s `postinstall` runs
`bun install --cwd ../ui --frozen-lockfile --production` right after it makes
the symlink, so `cd web && bun install` is enough and CI, the container builds
and Cloudflare Pages need no extra step. If you ever install with
`--ignore-scripts`, run that command yourself.

`--production` means a web-side install does NOT give you this package's
devDependencies. Working on the library is `cd ui && bun install`.

## Custom dropdowns

`Select` accepts an `options` array of `{ value, label }` objects. Set
`searchable` to add a search field; it defaults to off. Options can also carry
`description`, `group`, `disabled`, `searchText` (aliases), and `triggerLabel`.
Both adapters use the shared picker styling, with a modal sheet on mobile.

```svelte
<script lang="ts">
  import { Select } from '@bagel/ui/svelte';
  let zone = $state('America/Toronto');
  const options = [
    { value: 'America/Toronto', label: 'Toronto', description: 'America/Toronto' },
    { value: 'Europe/Paris', label: 'Paris', description: 'Europe/Paris' }
  ];
</script>

<Select name="timezone" label="Timezone" bind:value={zone} {options} searchable />
```

Pass localized `searchPlaceholder`, `searchClearLabel`, and `emptyLabel` from
the host application. Svelte also accepts `filterOptions` for domain-specific
ranking or aliases, as used by the dashboard's timezone wrapper. A native
fallback preserves form submission without JavaScript; after enhancement it
bridges validation, form reset, and real `input`/`change` events. Change handlers
receive an `HTMLSelectElement` as `currentTarget`, with bindings and form data
updated before the handler runs. Astro additionally supports native option
slots for existing callers. The adapters have separate SSR contract tests
because Svelte pre-renders its trigger and Astro creates it during enhancement.

## API conventions

Both adapters follow the same prop vocabulary. `CATALOG.md` lists each block's
props and whether it ships one adapter or both (`stable` or `svelte-only`).

| Topic | Convention |
| --- | --- |
| `variant` vs `tone` | `variant` picks a shape or emphasis (`primary`, `secondary`, `display`). `tone` picks a colour from `neutral \| accent \| warm \| success \| warning \| danger \| info`; each component accepts a subset, typed in `lib/tone.ts`. |
| Accessible names | `label` names the component; `<part>Label` names a part (`searchClearLabel`, `closeLabel`). Defaults come from locales, see below. |
| Callbacks | Native DOM events keep their lowercase names (`onclick`, `oninput`). Semantic callbacks are camelCase and receive a value: `onSelect`, `onClose`, `onCheckedChange`, `onValueChange`, `onOpenChange`. When `onClose` is given it owns the open state. |
| Bindables | `checked`, `value` and `open` are `$bindable`. |
| State flags | `busy` for work in progress, `pressed` for toggle buttons, `current` for the active item in a set. |
| Slots | Svelte snippets and Astro slots share names: `actions`, `footer`, `leading`, `trailing`. |
| `class` | Always merged with the component's own classes, never replaced. |
| Rest props | Unknown attributes land on the root element, or on the native control for form inputs. |
| `as` | Typed to the elements the block supports. Svelte layout blocks are generic over the element, so `Cluster as="form"` accepts `method` and `action`; `Heading` and `Section` take a fixed union. Astro takes a union and types rest props as `div` attributes. |
| Locale defaults | Built-in strings come from `locales/<code>/<ns>.json`, resolved by `setUiI18n` (Svelte) or `uiI18n(Astro)` (Astro). A prop always overrides the default. |

## Checks

```bash
cd ui
bun install --frozen-lockfile
bun run check   # tsc --noEmit + assert-framework-free.mjs
bun run test    # parity test + per-entry gzip budgets
```

`svelte-check` runs as part of `check`, from the first adapter pair onwards.
Two things about it are worth knowing before you trust its output:

- it needs a `svelte.config.js` to exist at all, even an empty default export;
- it decides what to check from the **`include` list in `tsconfig.json`**. With
  only `*.ts` globs there it reports `0 ERRORS` over hundreds of files while
  checking no component whatsoever. `svelte/**/*.svelte` is in the list for
  that reason, and the way to confirm it still works is to plant a type error
  in an adapter and watch the file count go up by one.

The Astro adapters have no standalone type check here either; the marketing
build is their gate. The parity test does render them: `test/loaders.ts`
compiles `.astro` through `@astrojs/compiler` and `.svelte` through
`svelte/compiler` for `bun test`, which is why `astro` is a devDependency. It
never reaches a console image — the Containerfiles install this package with
`--production`, which leaves only `lenis`.
