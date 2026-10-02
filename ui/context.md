# Design library context

Use [shared terminology](../CONTEXT.md) and the [context map](../CONTEXT-MAP.md).
`@bagel/ui` is a standalone source package with its own lockfile and checks, outside `web/`.

## Responsibility and boundaries

- Supplies generic CSS contracts, framework-free browser engines, Svelte adapters and Astro adapters.
- Owns reusable controls/layout/chrome, animation effects and shared self-hosted fonts.
- Knows no bot modules, accounts, Premium policy, RPC subjects or session permissions.
- [Kit](../web/kit/context.md) composes these primitives with bot-specific navigation/session behavior.
- Shared component contracts belong here; app-specific copy, links and permissions belong to consumers.
- `web/package.json` postinstall links `web/node_modules/@bagel/ui` to this directory.
- UI is not a web workspace member; install its development dependencies here when editing the library.

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Block / primitive | Reusable visual component listed in the generated catalog. |
| Contract stylesheet | Shared element class/attribute/custom-property rules used by both adapters. |
| Adapter | Framework-specific markup/lifecycle wrapper over the shared contract/engine. |
| Engine | Framework-free browser behavior in `lib/`, usually mounted with a disposal function. |
| Parity | Svelte/Astro render agreement checked by normalized markup contracts and fixtures. |
| Inspector | Generic editor state machine tracking committed/draft/saving/error state. |
| Overlay | Modal/popover/picker surface coordinated by shared stack/focus/portal helpers. |
| Picker panel | Anchored desktop selection surface that can become a mobile modal sheet. |
| Layer | CSS cascade tier: tokens → base → elements → app overrides. |
| Golden | Committed normalized HTML fixture used to detect adapter contract drift. |

These are presentation concepts; an application's module "surface" and a UI surface are different uses.
Default UI strings live in `locales/<lang>/<namespace>.json`; props can override them.
Svelte `setUiI18n` and Astro `uiI18n(Astro)` resolve locale without product-specific copy.

## Code navigation

- [CATALOG.md](CATALOG.md): fastest component lookup: family, props, adapters and contract file.
- [package.json](package.json): extension-specific component/style/font wildcards, explicit public
  engine/state subpaths, optional framework peers and CSS side effects.
- [styles/layers.css](styles/layers.css): canonical `bb.tokens`, `bb.base`, `bb.elements`, `bb.app` order.
- `styles/brand.css`, `semantic.css`, `fonts.css`, `a11y.css`: primitive/role tokens,
  font faces and accessibility policies.
- `styles/elements/`: component contracts; adapters import their own contract CSS.
- `svelte/` and `astro/`: named component adapters; `index.ts` files provide aggregate exports.
- [svelte/actions.ts](svelte/actions.ts): `use:` wrappers over shared reveal/decode/magnetic engines.
- `lib/select.ts` and `lib/astro-select.ts`: option filtering/navigation and Astro enhancement.
- [lib/overlay-stack.ts](lib/overlay-stack.ts): overlay order, portals, anchors, containment and focus traps.
- [lib/inspector-machine.ts](lib/inspector-machine.ts): pure draft/save transitions;
  `svelte/inspector.svelte.ts` and `discard-guard.svelte.ts` adapt state/navigation behavior.
- `lib/light-field.ts`, `cursor-engine.ts`, `reveal.ts`, `decode.ts`, `magnetic.ts`:
  reusable visual effects; keep mount/dispose cleanup and reduced-motion behavior intact.
- [lib/raf-loop.ts](lib/raf-loop.ts): shared animation subscription/wake scheduling.
- `lib/lenis.ts`, `scroll-spy.ts`, `reading-progress.ts`, `rail-glide.ts`: navigation/scroll engines.
- `lib/icons.ts`, `line-series.ts`, `csv.ts`, `clipboard.ts`: shared symbols/data/utility primitives.
- `lib/motion-gate.ts`, `tilt.ts`, `nested-scroll.ts`, `roving-focus.ts`: motion ownership/visibility,
  pointer effects, nested scroll and keyboard navigation. Inspect these before adding new listeners.
- `svelte/forms.ts`: shared form enhancements; `locales/index.ts` is generated UI catalog data.
- `fonts/`: local WOFF2 assets; consumers can import URLs for selective preloads.
- `test/loaders.ts`: test-time framework compilation; `test/normalise.ts` normalizes parity output.
- `test/__golden__/`: committed render contracts; update only for intentional markup changes.

## Component change workflow

- Find the block in the catalog; inspect the contract CSS and both adapters before changing props/markup.
- Keep behavior in an engine when multiple framework adapters need it.
- Keep framework lifecycle and syntax in adapters; `lib/` and `styles/` cannot import frameworks/node builtins.
- New blocks need classification in `scripts/gen-catalog.mjs`; single-adapter blocks need an explicit reason.
- Regenerate catalog/export metadata with `bun scripts/gen-catalog.mjs`; review committed output.
- Web consumers import component files directly (for example `@bagel/ui/svelte/Button.svelte`);
  `ui-barrel-not-imported.test.ts` rejects UI barrel imports under web. Barrels remain for external users.
- Public engines need explicit package exports; private `lib/` helpers are not exposed by a wildcard.

## Invariants and pitfalls

- `ui` must never import `@bagel/kit`; `assert-framework-free.mjs` enforces dependency direction.
- Declare layer order before component styles; app overrides belong to `bb.app` rather than specificity fights.
- Preserve Select's native fallback/form reset/validation/input/change semantics through enhancement.
- Use localized picker defaults or explicit caller overrides for placeholders/clear/empty labels.
- Some adapters intentionally have no twin; use catalog reasons rather than adding a useless static store UI.
- Engines must dispose listeners/frame work after unmount; SSR must not read browser globals at module load.
- Each adapter must import CSS for its rendered `bb-*` contracts; own-CSS and token gates enforce this.
- CSS side-effect declarations permit unused adapters to tree-shake while preserving imported contract styles.
- A web install gives ui production dependencies only; missing local dev tools requires an install in `ui/`.
- Astro adapters have render parity tests but no standalone Astro type-check script; consumer build is a gate.
- `styles/elements/fit.css` + `lib/text-fit.ts`: a `data-fit` span scales its font down to its box (parent or
  `data-fit-box` ancestor); the box owns geometry. Fit text stays hidden until fitted, so a consumer must mount
  the engine on every page that uses the attribute.

## Focused commands

From `ui`: `bun install --frozen-lockfile`, `bun run check`, `bun run test`.
Check runs TypeScript/Svelte, framework/token/own-CSS/layer/motion/control-name gates,
and generated locale/catalog freshness (`bun scripts/gen-locales.mjs` regenerates UI locale data).
Test runs Bun tests and per-entry gzip budgets; for picker work use `bun test test/select*.test.ts`.
For shared markup use `bun test test/parity.test.ts`; inspect intentional golden diffs.
For Astro integration also build the relevant consumer from `web/marketing` or `web/docs`.
