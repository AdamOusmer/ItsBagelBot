// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The normaliser both halves of the parity suite compare through. Extracted
// from parity.test.ts when the golden files arrived: the goldens are produced
// by this exact function, so a second copy of it anywhere would let a golden
// and the test that checks it disagree about what counts as markup.

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
 *  5. Void elements. Svelte's SSR closes `<img>` and `<br>` as `<img … />`,
 *     Astro emits `<img …>`. Both are valid HTML5 and parse to the identical
 *     node; the slash is not markup, it is a compiler's habit. Dropped from
 *     both, which also collapses the self-closing `<path …/>` inside an icon
 *     body -- harmless, because that body is one generated string both
 *     adapters print verbatim.
 *  6. Astro's hoisted `<script type="module" src="…">` tag. Both adapters
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
export function normalise(html: string): string {
  return html
    .replace(/<script type="module" src="[^"]*"><\/script>/g, '')
    .replace(/\s*\/>/g, '>')
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\s*=\s*(""|'')/g, '')
    .replace(/>\s+</g, '><')
    .replace(/\s+/g, ' ')
    .trim();
}

