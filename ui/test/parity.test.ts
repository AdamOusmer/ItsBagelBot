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
// harness's own smoke test, and the only case that still passes when every
// real element is broken, which is what tells you the failure is in an element
// and not in the loader plugins. Real element pairs live below: Cursor,
// LightField and NavLink as hand-written cases, the button/card contracts in
// their describe() blocks, and the rest in the PAIRS table.

import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import SvelteFixture from './fixture.svelte';
import AstroFixture from './fixture.astro';
import SvelteCursor from '../svelte/Cursor.svelte';
import AstroCursor from '../astro/Cursor.astro';
import SvelteLightField from '../svelte/LightField.svelte';
import AstroLightField from '../astro/LightField.astro';
import SvelteNavLink from '../svelte/NavLink.svelte';
import AstroNavLink from '../astro/NavLink.astro';
import SvelteBadge from '../svelte/Badge.svelte';
import AstroBadge from '../astro/Badge.astro';
import SvelteChip from '../svelte/Chip.svelte';
import AstroChip from '../astro/Chip.astro';
import SvelteEmptyState from '../svelte/EmptyState.svelte';
import AstroEmptyState from '../astro/EmptyState.astro';
import SvelteField from '../svelte/Field.svelte';
import AstroField from '../astro/Field.astro';
import SvelteSearchInput from '../svelte/SearchInput.svelte';
import AstroSearchInput from '../astro/SearchInput.astro';
import SvelteStatTile from '../svelte/StatTile.svelte';
import AstroStatTile from '../astro/StatTile.astro';
import SvelteSwitch from '../svelte/Switch.svelte';
import AstroSwitch from '../astro/Switch.astro';

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
 *  6. Void elements: Svelte serialises `<input …/>`, Astro `<input …>`. Both
 *     parse to the same node -- HTML has no self-closing syntax for void
 *     elements and the slash is ignored -- so the slash goes. Measured on
 *     SearchInput, 2026-09-09: the ONLY difference between its two adapters.
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
    .replace(/\s*\/>/g, '>')
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
// ── NavLink ────────────────────────────────────────────────────────────────
// The first adapter pair carrying PROPS, which is what makes it the first one
// the harness can drift on: Cursor and LightField above emit fixed markup.
//
// Three cases rather than one, because the three shapes NavLink can take are
// exactly the three places two adapters drift: the plain link (does Astro emit
// the same attribute order), the current + CTA link (do both suppress the
// attributes they are not given, rather than emitting target="undefined" or an
// empty aria-current — Astro drops an undefined attribute, Svelte drops it for
// `undefined` but emits it for `null`, and the two rules are not the same rule),
// and the disabled entry (a different ELEMENT, plus an extra child).
//
// External is folded into the CTA case on purpose: target and rel are the pair
// most likely to be typed by hand on one side only.

/** The contract. Edit deliberately; both adapters are held to it. */
const NAV_LINK_HTML =
  '<a class="bb-nav-link" href="/pricing">' +
  '<span class="bb-nav-link__label">Pricing</span></a>';

// The CTA carries the BUTTON contract's classes as well as its own: since the
// button element landed in this package, `--cta` composes `.bb-btn --go-solid`
// rather than hand-copying its fill. Class order is part of the contract here
// the same way attribute order is -- neither adapter sorts.
const NAV_LINK_CTA_HTML =
  '<a class="bb-nav-link bb-nav-link--cta bb-btn bb-btn--go-solid" href="https://example.test/add" ' +
  'aria-current="page" target="_blank" rel="noopener noreferrer">' +
  '<span class="bb-nav-link__label">Add</span></a>';

const NAV_LINK_DISABLED_HTML =
  '<span class="bb-nav-link bb-nav-link--block" aria-disabled="true">' +
  '<span class="bb-nav-link__label">Loyalty</span>' +
  '<span class="bb-nav-link__hint">Broadcaster only</span></span>';

const NAV_LINK_CASES: { name: string; props: Record<string, unknown>; html: string }[] = [
  {
    name: 'rail link',
    props: { href: '/pricing', label: 'Pricing' },
    html: NAV_LINK_HTML,
  },
  {
    name: 'current external cta',
    props: {
      href: 'https://example.test/add',
      label: 'Add',
      variant: 'cta',
      current: true,
      external: true,
    },
    html: NAV_LINK_CTA_HTML,
  },
  {
    name: 'disabled block entry',
    props: {
      label: 'Loyalty',
      block: true,
      disabled: true,
      hint: 'Broadcaster only',
    },
    html: NAV_LINK_DISABLED_HTML,
  },
];

for (const testCase of NAV_LINK_CASES) {
  test(`NavLink svelte adapter emits the contract markup: ${testCase.name}`, () => {
    const { body } = render(SvelteNavLink, { props: testCase.props });
    expect(normalise(body)).toBe(testCase.html);
  });

  test(`NavLink astro adapter emits the contract markup: ${testCase.name}`, async () => {
    const container = await experimental_AstroContainer.create();
    const html = await container.renderToString(AstroNavLink, {
      props: testCase.props,
    });
    expect(normalise(html)).toBe(testCase.html);
  });

  test(`NavLink adapters agree: ${testCase.name}`, async () => {
    const container = await experimental_AstroContainer.create();
    const svelte = normalise(render(SvelteNavLink, { props: testCase.props }).body);
    const astro = normalise(
      await container.renderToString(AstroNavLink, { props: testCase.props }),
    );
    expect(svelte).toBe(astro);
  });
}
/* ── button + card (PR8) ──────────────────────────────────────────────────
 *
 * The first real element pairs. Each case renders the SAME props through both
 * adapters and asserts three things: the Svelte output, the Astro output, and
 * that they are equal. The literal on the right is the contract -- editing one
 * is a deliberate act, and nothing here regenerates itself.
 *
 * Rendered through ./fixtures/* rather than the adapters directly because
 * children are a snippet on one side and a slot on the other, and hand-writing
 * a Svelte snippet in the test would be testing the test.
 */

import { describe } from 'bun:test';
import SvelteButton from './fixtures/button.svelte';
import AstroButton from './fixtures/button.astro';
import SvelteCard from './fixtures/card.svelte';
import AstroCard from './fixtures/card.astro';
import SvelteCardHead from './fixtures/cardhead.svelte';
import AstroCardHead from './fixtures/cardhead.astro';

/* Both adapters are `any` here, the same way types/components.d.ts declares
 * them: naming svelte's `Component<Props>` or Astro's `AstroComponentFactory`
 * would make this package's type check depend on a framework, which is what
 * scripts/assert-framework-free.mjs exists to prevent. The assertions are on
 * rendered HTML, so nothing is lost by the components being opaque to tsc. */
/* eslint-disable @typescript-eslint/no-explicit-any */
type Adapter = any;

/** Render both adapters of one element with identical props. */
async function pair(
  svelteComponent: Adapter,
  astroComponent: Adapter,
  props: Record<string, unknown>,
): Promise<{ svelte: string; astro: string }> {
  const container = await experimental_AstroContainer.create();
  return {
    svelte: normalise(render(svelteComponent, { props }).body),
    astro: normalise(await container.renderToString(astroComponent, { props })),
  };
}

/** One contract case: both adapters emit `html`, and they agree. */
interface Case {
  /** What this case is asserting, as the test name. */
  name: string;
  /** The Svelte adapter (or a fixture that renders it). */
  svelte: Adapter;
  /** The Astro adapter (or a fixture that renders it). */
  astro: Adapter;
  /** Handed to both adapters unchanged -- that is the point of the test. */
  props: Record<string, unknown>;
  /** The contract. Edit deliberately. */
  html: string;
}

function contract({ name, svelte, astro, props, html }: Case) {
  test(name, async () => {
    const rendered = await pair(svelte, astro, props);
    expect(rendered.svelte).toBe(html);
    expect(rendered.astro).toBe(html);
  });
}

describe('Button', () => {
  contract({
    name: 'default variant is primary',
    svelte: SvelteButton,
    astro: AstroButton,
    props: {},
    html: '<button class="bb-btn bb-btn--primary" type="button" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></button>',
  });

  contract({
    name: 'ghost at sm',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'ghost', size: 'sm' },
    html: '<button class="bb-btn bb-btn--ghost bb-btn--sm" type="button" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></button>',
  });

  contract({
    name: 'green solid, the one filled button',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'green', solid: true },
    html: '<button class="bb-btn bb-btn--green bb-btn--solid" type="button" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></button>',
  });

  contract({
    name: 'destructive submit',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'destructive', type: 'submit', class: 'row-act' },
    html: '<button class="bb-btn bb-btn--destructive row-act" type="submit" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></button>',
  });

  // loading sets native disabled as well as aria-busy: a busy control that is
  // still focusable and still submits is the bug this pair exists to prevent.
  contract({
    name: 'loading is disabled and busy, with a real spinner element',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { loading: true },
    html: '<button class="bb-btn bb-btn--primary is-loading" type="button" disabled aria-busy="true" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span><span class="bb-btn__spinner" aria-hidden="true"></span></button>',
  });

  contract({
    name: 'done',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { done: true },
    html: '<button class="bb-btn bb-btn--primary is-done" type="button" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></button>',
  });

  // The icon variant emits no mark at all -- that is the reason the mark is a
  // real element rather than the ::before both sources used.
  contract({
    name: 'icon-only carries no mark and an author-supplied name',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'icon', icon: true, label: 'Close' },
    html: '<button class="bb-btn bb-btn--icon" type="button" data-mark aria-label="Close"><span class="bb-btn__content"><svg viewBox="0 0 24 24"></svg></span></button>',
  });

  // Divergence BL1: Astro switches on `href` inside one component, Svelte has
  // a second component. Same markup either way, which is what makes that
  // acceptable.
  contract({
    name: 'href renders an anchor (Astro Button) == ButtonLink (Svelte)',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { link: true, variant: 'secondary' },
    html: '<a class="bb-btn bb-btn--secondary" href="/x" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></a>',
  });
});

describe('Card', () => {
  // Flat: no body wrapper (C6), no atmosphere, no hover.
  contract({
    name: 'flat card renders children directly',
    svelte: SvelteCard,
    astro: AstroCard,
    props: {},
    html: '<div class="bb-card" data-card>body</div>',
  });

  contract({
    name: 'atmosphere and hover are opt-in attributes, not page classes',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { atmo: true, hover: true, label: '#lofi' },
    html: '<div class="bb-card" data-card data-atmo data-hover><div class="bb-card-atmo" aria-hidden="true"><span class="bb-card-atmo__ring"></span><span class="bb-card-atmo__sheen"></span></div><span class="bb-card__label">#lofi</span>body</div>',
  });

  contract({
    name: 'banded card grows a housing and a padded body',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { banded: true, atmo: true, label: '01' },
    html: '<div class="bb-card bb-card--band" data-card data-atmo><span class="bb-card__band"><div class="bb-card-atmo" aria-hidden="true"><span class="bb-card-atmo__ring"></span><span class="bb-card-atmo__sheen"></span></div><span class="bb-card__label">01</span><span class="bb-card__band-inner"><i>B</i></span></span><span class="bb-card__body">body</span></div>',
  });

  contract({
    name: 'stat and sheen modifiers, and an anchor card',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { as: 'a', href: '/x', stat: true, sheen: true, class: 'tile' },
    html: '<a class="bb-card bb-card--stat bb-card--sheen tile" href="/x" data-card>body</a>',
  });
});

describe('CardHead', () => {
  contract({
    name: 'title only',
    svelte: SvelteCardHead,
    astro: AstroCardHead,
    props: {},
    html: '<div class="bb-card-head"><h3 class="bb-card-head__title">Recent</h3></div>',
  });

  contract({
    name: 'with an action link on the contract class',
    svelte: SvelteCardHead,
    astro: AstroCardHead,
    props: { withAction: true },
    html: '<div class="bb-card-head"><h3 class="bb-card-head__title">Recent</h3><a class="bb-card-head__more" href="/x">All</a></div>',
  });
});

/**
 * Real element pairs.
 *
 * `html` is the golden: the exact contract markup both adapters owe, written
 * out here so a change to what an element emits is an edit to this file. The
 * CSS in ui/styles/elements/ selects on these class names and data attributes,
 * so this table is also the readable index of what the contract IS.
 *
 * `slot` is the default-slot content, given to Astro as a slot string and to
 * Svelte as a raw snippet. createRawSnippet is the only way to hand a server
 * render a `children` without writing a wrapper .svelte per element; its
 * `render()` output is emitted verbatim in SSR, which is what makes the two
 * halves comparable at all.
 */
const PAIRS: {
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
  slot?: string;
  html: string;
}[] = [
  {
    name: 'Badge',
    svelte: SvelteBadge,
    astro: AstroBadge,
    props: { tone: 'live', mark: 'solid', status: true },
    slot: 'Live',
    html:
      '<span class="bb-tag bb-badge bb-tag--live" role="status">' +
      '<i class="bb-mark" aria-hidden="true"></i>Live</span>',
  },
  {
    name: 'Chip',
    svelte: SvelteChip,
    astro: AstroChip,
    props: { on: true, tone: 'muted' },
    slot: 'Timers',
    html: '<button type="button" class="bb-chip bb-chip--muted" data-on>Timers</button>',
  },
  {
    name: 'EmptyState',
    svelte: SvelteEmptyState,
    astro: AstroEmptyState,
    props: { title: 'No timers yet', body: 'Add one to get started.' },
    slot: 'cta',
    html:
      '<div class="bb-empty"><p class="bb-empty__title">No timers yet</p>' +
      '<p class="bb-empty__body">Add one to get started.</p>' +
      '<div class="bb-empty__cta">cta</div></div>',
  },
  {
    name: 'Field',
    svelte: SvelteField,
    astro: AstroField,
    props: { label: 'Cooldown', tag: 'default: 5', hint: 'Seconds.', hintId: 'h1' },
    slot: 'control',
    html:
      '<label class="bb-field"><span class="bb-field__label">Cooldown' +
      '<small class="bb-tag bb-tag--quiet bb-field__tag">default: 5</small></span>' +
      // The spaces around the slot are content, not layout: they are the
      // newline+indent between </span> and <slot/> in both templates, and a
      // text node between two inline elements renders as a space. Both
      // adapters emit them; normalising them away would hide a real
      // difference in a case where one of them stopped.
      ' control <small class="bb-field__hint" id="h1">Seconds.</small></label>',
  },
  {
    name: 'SearchInput',
    svelte: SvelteSearchInput,
    astro: AstroSearchInput,
    props: { placeholder: 'Find a command' },
    html:
      '<label class="bb-search bb-input">' +
      '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle>' +
      '<path d="m20 20-3.5-3.5"></path></svg>' +
      '<input type="search" class="bb-search__input" placeholder="Find a command" value>' +
      '</label>',
  },
  {
    name: 'StatTile',
    svelte: SvelteStatTile,
    astro: AstroStatTile,
    props: { label: 'Messages', value: '12,904', unit: 'msg', delta: '+4.1%' },
    html:
      '<div class="bb-stat"><div class="bb-stat__head">' +
      '<span class="bb-stat__label">Messages</span></div>' +
      '<div class="bb-stat__value"><span data-count-up>12,904</span><small>msg</small></div>' +
      '<div class="bb-stat__delta">+4.1%</div></div>',
  },
  {
    name: 'Switch',
    svelte: SvelteSwitch,
    astro: AstroSwitch,
    props: { checked: true, label: 'Enable timers', describedby: 'h2' },
    html:
      '<button type="button" class="bb-switch" role="switch" aria-checked="true" ' +
      'aria-label="Enable timers" aria-describedby="h2" data-state="on"></button>',
  },
];

for (const pair of PAIRS) {
  test(`${pair.name}: svelte adapter emits the contract markup`, () => {
    const props = { ...pair.props } as Record<string, unknown>;
    if (pair.slot !== undefined) {
      props.children = createRawSnippet(() => ({ render: () => pair.slot as string }));
    }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const { body } = render(pair.svelte as any, { props });
    expect(normalise(body)).toBe(pair.html);
  });

  test(`${pair.name}: astro adapter emits the contract markup`, async () => {
    const container = await experimental_AstroContainer.create();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const html = await container.renderToString(pair.astro as any, {
      props: pair.props,
      ...(pair.slot === undefined ? {} : { slots: { default: pair.slot } }),
    });
    expect(normalise(html)).toBe(pair.html);
  });
}

/**
 * VIP is SILVER.
 *
 * It shipped purple (#d9aaff) once, and the house rule since is that VIP is
 * #dfe4e9 -- free green, paid gold, VIP silver. The perm ladder itself is bot
 * data and lives in @bagel/kit (PermBadge.svelte drives --badge-tone), so
 * there is no ui-side token to pin. What CAN be pinned here is that the
 * generic badge contract ships no colour ladder of its own to regress: the
 * only colours badge.css may name are the caller's custom property and the
 * neutral hairline fallback.
 */
test('badge.css ships no perm ladder and no purple', async () => {
  const css = await Bun.file(new URL('../styles/elements/badge.css', import.meta.url)).text();
  const rules = css.replace(/\/\*[\s\S]*?\*\//g, '');
  expect(rules).not.toMatch(/#d9aaff/i);
  expect(rules).not.toMatch(/everyone|broadcaster|lead_mod/);
});
