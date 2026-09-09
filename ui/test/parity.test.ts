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
// While ui/svelte/ and ui/astro/ are still empty this runs on a pair of
// fixtures. Each element pair joins it as it lands (Button and Card from the
// button-card PR onward).

import { expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import SvelteFixture from './fixture.svelte';
import AstroFixture from './fixture.astro';

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
 *
 * Attribute ORDER is deliberately not normalised. It does not affect rendering,
 * but it does affect diffs of the emitted HTML, and holding the two adapters to
 * the same order is free as long as they are written from the same contract.
 * If a framework ever reorders on its own this comment is the place to record
 * that it was measured, and the sort belongs here.
 */
function normalise(html: string): string {
  return html
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
