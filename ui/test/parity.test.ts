// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Adapter parity: every element @bagel/ui ships for both frameworks must emit
// the SAME contract markup from its Svelte adapter and its Astro adapter.
//
// This is the test that makes "one design library, two adapters" a fact rather
// than an intention. The contract lives in CSS (ui/styles/elements/*.css) and
// selects on class names and data attributes; an adapter that drifts by one
// class does not fail to compile, does not fail a type check, and does not look
// wrong on the surface its author was working on. It looks wrong on the other
// one, weeks later. The duplication this package exists to delete was created
// exactly that way.
//
// Golden-output shape (see the golden-output-tests skill): the expected markup
// is written out below as a literal, so changing what an element emits is a
// deliberate edit to this file and not a side effect of running the suite.
// Nothing here regenerates itself.
//
// Both halves really render. The Astro half was expected to be the hard one --
// the container API is documented for vitest, and .astro files need Astro's own
// compiler pass before any runner can import one -- but measured on
// 2026-09-09 it runs under `bun test` with a 12-line @astrojs/compiler onLoad
// plugin (test/loaders.ts): fixture rendered, output
// `<p class="bb-fixture" data-fixture>hello</p>`. That is why `astro` and
// `@astrojs/compiler` are devDependencies of this package. They are dev-only
// and never enter a console image: the Containerfiles install ui with
// `--production`.
//
// The fixture pair stays even though real adapters have landed: it is the
// harness's own test. When Cursor and LightField both fail, the fixture row
// says whether the compilers broke or the elements did.

import { expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import SvelteFixture from './fixture.svelte';
import AstroFixture from './fixture.astro';
import SvelteCursor from '../svelte/Cursor.svelte';
import AstroCursor from '../astro/Cursor.astro';
import SvelteLightField from '../svelte/LightField.svelte';
import AstroLightField from '../astro/LightField.astro';

/**
 * Reduce rendered HTML to the part the CSS contract actually selects on.
 *
 * Four differences are framework bookkeeping, not markup, and every one of them
 * would otherwise fail a pair that is genuinely identical:
 *
 *  1. Svelte's SSR output wraps each component in `<!--[-->` / `<!--]-->`
 *     hydration markers. They are instructions to Svelte's client runtime and
 *     never reach the CSSOM. All comments go.
 *  2. Svelte serialises a boolean attribute as `data-fixture=""`; Astro emits it
 *     bare as `data-fixture`. `[data-fixture]` matches both. Empty values are
 *     dropped so the two spellings converge on the bare form.
 *  3. Indentation and newlines between tags differ with how each compiler lays
 *     out its template. Whitespace BETWEEN tags collapses away; whitespace
 *     inside a text node is collapsed to single spaces but kept, because that
 *     is content.
 *  4. Trailing/leading whitespace around the whole fragment.
 *  5. Astro's hoisted `<script type="module" src="…">` tag. Both adapters
 *     attach the same engine, but they say so in different places: Astro emits
 *     a tag into the HTML and lets the bundler rewrite the src, while Svelte
 *     compiles the `$effect` into the component's own JS and emits nothing.
 *     The tag is build bookkeeping (its src here is the raw source path,
 *     because this harness runs @astrojs/compiler without Astro's Vite plugin),
 *     and no CSS contract can select on it. Only the empty, src-carrying form
 *     is dropped: an inline `<script>` in an adapter WOULD be markup, and this
 *     leaves it in the diff so it has to be argued for.
 *
 * Attribute ORDER is deliberately not normalised. It does not affect rendering,
 * but it does affect diffs of the emitted HTML, and holding the two adapters to
 * the same order is free as long as they are written from the same contract.
 * If a framework ever reorders on its own this comment is the place to record
 * that it was measured, and the sort belongs here.
 */
function normalise(html: string): string {
  return html
    .replace(/<script type="module" src="[^"]*"><\/script>/g, '')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\s*=\s*(""|'')/g, '')
    .replace(/>\s+</g, '><')
    .replace(/\s+/g, ' ')
    .trim();
}

/** The contract. Edit deliberately; both adapters are held to it. */
const FIXTURE_HTML = '<p class="bb-fixture" data-fixture>hello</p>';

test('svelte adapter emits the contract markup', () => {
  const { body } = render(SvelteFixture, { props: { text: 'hello' } });
  expect(normalise(body)).toBe(FIXTURE_HTML);
});

test('astro adapter emits the contract markup', async () => {
  const container = await experimental_AstroContainer.create();
  const html = await container.renderToString(AstroFixture, {
    props: { text: 'hello' },
  });
  expect(normalise(html)).toBe(FIXTURE_HTML);
});

test('the two adapters agree', async () => {
  const container = await experimental_AstroContainer.create();
  const svelte = normalise(
    render(SvelteFixture, { props: { text: 'parity' } }).body,
  );
  const astro = normalise(
    await container.renderToString(AstroFixture, { props: { text: 'parity' } }),
  );
  expect(svelte).toBe(astro);
});

/**
 * The custom cursor: two fixed layers the engine writes geometry onto. The
 * Svelte adapter renders them behind an `enabled` prop (the console gates on a
 * user preference); the default is on, which is the Astro adapter's only
 * behaviour, so the default is what is compared.
 */
const CURSOR_HTML =
  '<div class="bb-cursor" aria-hidden="true"></div>' +
  '<div class="bb-cursor-ring" aria-hidden="true"></div>';

test('cursor: both adapters emit the contract markup', async () => {
  const container = await experimental_AstroContainer.create();
  const svelte = normalise(render(SvelteCursor, { props: {} }).body);
  const astro = normalise(await container.renderToString(AstroCursor, { props: {} }));

  expect(svelte).toBe(CURSOR_HTML);
  expect(astro).toBe(CURSOR_HTML);
});

test('cursor: the svelte adapter renders nothing when disabled', () => {
  // Not a parity case — Astro has no `enabled` prop, because the marketing site
  // has no cursor preference to gate on. It is here because "disabled" must
  // mean "no elements at all", not "elements the engine ignores": leaving them
  // in the tree would leave `cursor: none` fighting a native pointer.
  const { body } = render(SvelteCursor, { props: { enabled: false } });
  expect(normalise(body)).toBe('');
});

/**
 * The mote field. `data-field` and `data-warmth` are contract attributes even
 * though only the Astro adapter reads them back off the DOM; see the note in
 * ui/svelte/LightField.svelte.
 */
const LIGHT_FIELD_HTML =
  '<canvas class="bb-light-field" data-field data-warmth="0.7" aria-hidden="true"></canvas>';

test('light field: both adapters emit the contract markup', async () => {
  const container = await experimental_AstroContainer.create();
  const svelte = normalise(render(SvelteLightField, { props: {} }).body);
  const astro = normalise(await container.renderToString(AstroLightField, { props: {} }));

  expect(svelte).toBe(LIGHT_FIELD_HTML);
  expect(astro).toBe(LIGHT_FIELD_HTML);
});

test('light field: the class and warmth props agree', async () => {
  const props = { class: 'bb-light-field--bleed', warmth: 0.4 };
  const container = await experimental_AstroContainer.create();
  const svelte = normalise(render(SvelteLightField, { props }).body);
  const astro = normalise(await container.renderToString(AstroLightField, { props }));

  expect(svelte).toBe(
    '<canvas class="bb-light-field bb-light-field--bleed" data-field data-warmth="0.4" aria-hidden="true"></canvas>',
  );
  expect(astro).toBe(svelte);
});
