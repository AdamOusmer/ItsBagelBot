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

### Subpath exports are wildcards, on purpose

`package.json` maps `./styles/*`, `./fonts/*`, `./lib/*`, `./svelte/*` and
`./astro/*` rather than listing every file the way `@bagel/kit` does. JSON has
no comments, so the reason is here: this library is filled in by a stack of
parallel pull requests, each adding files under those directories. With
explicit entries, every one of those branches edits the same object in the same
file and every one of them conflicts with its siblings. With wildcards, adding
a file is adding a file.

`./lib/*` maps to `./lib/*.ts` so consumers write `@bagel/ui/lib/light-field`
without the extension, matching how kit's explicit entries read. The other
three keep the extension, because a stylesheet, a font and a component are
imported by full filename anyway.

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
