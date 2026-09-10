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
import { normalise } from './normalise';
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

// ── Nav, footer and shell ──────────────────────────────────────────────────
// Fifteen pairs, checked against a committed golden file each rather than
// against a literal in this file. Two reasons, and the second is why the
// earlier elements above keep their literals:
//
//  1. Size. The rail's contract markup is 3.3 KB; four such literals would
//     bury the twelve cases above them in this file.
//  2. Blast radius. One file per element means a deliberate change to one
//     element's markup is a one-file diff a reviewer can read, instead of a
//     hunk inside a 400-line test.
//
// Nothing here regenerates: the goldens are read, never written. They were
// produced once by a script pointed at the same `CASES` registry, and that
// script is deliberately NOT in the repo -- a fixture that rewrites itself on
// failure is a test that cannot fail. Re-baselining a deliberate markup change
// means running such a script by hand and committing the diff.
//
// Rail, Topbar and Dock are compared in their STATIC render: no glide
// measured, no clock ticking, no group popover open. Their engines
// (ui/lib/rail-glide, clock, dock-groups, hash-active) are framework-free and
// get their own unit tests; what parity is for is the markup the CSS selects
// on, which is what the two adapters can silently disagree about.
import { CASES } from './pr10-cases';

for (const parityCase of CASES) {
  const golden = (
    await Bun.file(
      new URL(`./__golden__/${parityCase.name}.html`, import.meta.url).pathname,
    ).text()
  ).trim();

  test(`${parityCase.name}: svelte adapter matches the golden`, () => {
    // The cast is the price of ONE registry driving two renderers: both
    // `render` and the Astro container type their props against the
    // component's own generic, and a heterogeneous case list has no single
    // generic to give them.
    const { body } = render(parityCase.svelte as never, {
      props: parityCase.props as never,
    });
    expect(normalise(body)).toBe(golden);
  });

  test(`${parityCase.name}: astro adapter matches the golden`, async () => {
    const container = await experimental_AstroContainer.create();
    const html = await container.renderToString(parityCase.astro as never, {
      props: parityCase.props as never,
    });
    expect(normalise(html)).toBe(golden);
  });
}


// ── The remaining elements ─────────────────────────────────────────────────
// Everything presentational that was still living in a consuming app: the
// console's overlays, rows, decks and chart chrome, and the marketing site's
// hero, headings, text links, page ornaments and reading bar.
//
// Registered as a table rather than as hand-written pairs, because at thirteen
// elements the interesting failure is no longer "this element drifted" but
// "somebody added an element and forgot the other adapter". A row per element
// with its props and its expected markup makes the omission visible in the
// diff of this file.
//
// Rows are the DEFAULT rendering plus, where an element's shape genuinely
// changes with a prop, the changed one. Not every prop: a table that asserts
// every combination stops being read.

import SvelteAlertBanner from '../svelte/AlertBanner.svelte';
import AstroAlertBanner from '../astro/AlertBanner.astro';
import SvelteDeckList from '../svelte/DeckList.svelte';
import AstroDeckList from '../astro/DeckList.astro';
import SvelteOverviewGrid from '../svelte/OverviewGrid.svelte';
import AstroOverviewGrid from '../astro/OverviewGrid.astro';
import SvelteManagementRow from '../svelte/ManagementRow.svelte';
import AstroManagementRow from '../astro/ManagementRow.astro';
import SvelteEditorFooter from '../svelte/EditorFooter.svelte';
import AstroEditorFooter from '../astro/EditorFooter.astro';
import SvelteSaveStatus from '../svelte/SaveStatus.svelte';
import AstroSaveStatus from '../astro/SaveStatus.astro';
import SvelteModal from '../svelte/Modal.svelte';
import AstroModal from '../astro/Modal.astro';
import SvelteInspectorSurface from '../svelte/InspectorSurface.svelte';
import AstroInspectorSurface from '../astro/InspectorSurface.astro';
import SveltePageHero from '../svelte/PageHero.svelte';
import AstroPageHero from '../astro/PageHero.astro';
import SvelteSectionHeading from '../svelte/SectionHeading.svelte';
import AstroSectionHeading from '../astro/SectionHeading.astro';
import SvelteTextLink from '../svelte/TextLink.svelte';
import AstroTextLink from '../astro/TextLink.astro';
import SvelteBrackets from '../svelte/Brackets.svelte';
import AstroBrackets from '../astro/Brackets.astro';
import SvelteReadingProgress from '../svelte/ReadingProgress.svelte';
import AstroReadingProgress from '../astro/ReadingProgress.astro';

const LIGHT_FIELD = '<canvas class="bb-light-field" data-field data-warmth="0.7" aria-hidden="true"></canvas>';

/** The contract, one row per element. Edit deliberately.
 *
 * Named REMAINING, not CASES: the nav/footer/shell block above already
 * imports a `CASES` registry from ./pr10-cases, and two registries in one
 * module scope cannot share a name. The rows are unchanged. */
const REMAINING: {
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
  html: string;
}[] = [
  {
    name: 'AlertBanner: danger',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: {},
    html: '<div class="bb-alert bb-alert--danger" role="alert"><span class="bb-alert__msg"></span></div>',
  },
  {
    // The staff bar: a different tone AND a different live region, which is
    // the pair most likely to be changed on one adapter only.
    name: 'AlertBanner: impersonation',
    svelte: SvelteAlertBanner,
    astro: AstroAlertBanner,
    props: { variant: 'impersonation', role: 'status' },
    html: '<div class="bb-alert bb-alert--impersonation" role="status"><span class="bb-alert__msg"></span></div>',
  },
  {
    name: 'DeckList: default',
    svelte: SvelteDeckList,
    astro: AstroDeckList,
    props: {},
    html: '<div class="bb-card bb-deck-list"></div>',
  },
  {
    // `as` is the landmark escape hatch; a deck rendered as a div loses its
    // labelled region, so the tag is contract.
    name: 'DeckList: as section',
    svelte: SvelteDeckList,
    astro: AstroDeckList,
    props: { as: 'section', class: 'timers' },
    html: '<section class="bb-card bb-deck-list timers"></section>',
  },
  {
    name: 'OverviewGrid: default',
    svelte: SvelteOverviewGrid,
    astro: AstroOverviewGrid,
    props: {},
    html: '<div class="bb-ov-row"><div class="bb-ov-row__main"></div><div class="bb-ov-row__side"></div></div>',
  },
  {
    name: 'ManagementRow: default',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: {},
    html:
      '<div class="bb-row row-shell">' +
      '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="false"></button>' +
      '</div>',
  },
  {
    // Selected + expanded + controls: the three attributes that make the row
    // announce its relationship to the inspector it opens.
    name: 'ManagementRow: selected',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { selected: true, expanded: true, controls: 'insp', disabled: true },
    html:
      '<div class="bb-row row-shell is-selected is-off">' +
      '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="true" aria-controls="insp" aria-current="true"></button>' +
      '</div>',
  },
  {
    name: 'EditorFooter: idle',
    svelte: SvelteEditorFooter,
    astro: AstroEditorFooter,
    props: {},
    html:
      '<div class="bb-editor-foot">' +
      '<span class="bb-editor-foot__status" role="status" aria-live="polite"></span>' +
      '<span class="bb-editor-foot__acts">' +
      '<button class="bb-btn bb-btn--ghost" type="button" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i>' +
      '<span class="bb-btn__content">Cancel</span></button>' +
      '<button class="bb-btn bb-btn--primary" type="submit" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i>' +
      '<span class="bb-btn__content">Save</span></button>' +
      '</span></div>',
  },
  {
    // `saving` is the one state that changes BOTH halves: the status label and
    // the submit button's own label and disabled flag.
    name: 'EditorFooter: saving',
    svelte: SvelteEditorFooter,
    astro: AstroEditorFooter,
    props: { status: 'saving', dirty: true },
    html:
      '<div class="bb-editor-foot">' +
      '<span class="bb-editor-foot__status" role="status" aria-live="polite">' +
      '<span class="bb-editor-foot__s bb-editor-foot__s--saving">Saving…</span></span>' +
      '<span class="bb-editor-foot__acts">' +
      '<button class="bb-btn bb-btn--ghost" type="button" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i>' +
      '<span class="bb-btn__content">Cancel</span></button>' +
      '<button class="bb-btn bb-btn--primary" type="submit" disabled data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i>' +
      '<span class="bb-btn__content">Saving…</span></button>' +
      '</span></div>',
  },
  {
    name: 'SaveStatus: live',
    svelte: SvelteSaveStatus,
    astro: AstroSaveStatus,
    props: { state: 'live' },
    html:
      '<span class="bb-tag bb-tag--live" role="status">' +
      '<i class="bb-mark" aria-hidden="true"></i>Synced to chat' +
      '<i class="bb-sweep" aria-hidden="true"></i></span>',
  },
  {
    name: 'SaveStatus: error compact',
    svelte: SvelteSaveStatus,
    astro: AstroSaveStatus,
    props: { state: 'error', compact: true },
    html:
      '<span class="bb-tag bb-tag--error" role="status">' +
      '<i class="bb-mark bb-mark--hollow" aria-hidden="true"></i></span>',
  },
  {
    // No `title`, on purpose: the Svelte adapter mints the title's id from
    // $props.id(), which is per-render and has no Astro equivalent. The
    // labelled form is asserted on its own below.
    name: 'Modal: labelled by aria-label',
    svelte: SvelteModal,
    astro: AstroModal,
    props: { open: true, ariaLabel: 'Celebration' },
    html:
      '<div class="bb-modal" data-overlay style="z-index: 200">' +
      '<button class="bb-modal__backdrop" type="button" aria-label="Close" data-cursor="quiet"></button>' +
      '<div class="bb-modal__card" role="dialog" aria-modal="true" tabindex="-1" aria-label="Celebration" data-lenis-prevent></div>' +
      '</div>',
  },
  {
    // The Svelte adapter renders the SHEET below 1080px, but only a browser
    // has a viewport: server-side the media query stands in with matches:false,
    // so both adapters render the docked panel and that is what parity covers.
    name: 'InspectorSurface: docked',
    svelte: SvelteInspectorSurface,
    astro: AstroInspectorSurface,
    props: { open: true, title: 'Timer', controls: 'insp-body' },
    html:
      '<aside class="bb-surface bb-card bb-surface--docked" aria-label="Timer">' +
      '<div class="bb-surface__head">' +
      '<span class="bb-surface__tag bb-tag bb-tag--bare">Timer</span>' +
      '<button class="bb-surface__close" type="button" aria-label="Close">' +
      '<svg class="bb-icon" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      '<path d="M18 6L6 18M6 6l12 12"></svg></button></div>' +
      '<div class="bb-surface__body" id="insp-body"></div></aside>',
  },
  {
    name: 'PageHero: full',
    svelte: SveltePageHero,
    astro: AstroPageHero,
    props: { eyebrow: 'Pricing', title: 'Simple pricing', description: 'One plan.' },
    html:
      '<header class="bb-page-hero">' +
      LIGHT_FIELD +
      '<div class="bb-page-hero__glow" aria-hidden="true"></div>' +
      '<div class="bb-page-hero__inner">' +
      '<span class="bb-page-hero__eyebrow">Pricing</span>' +
      '<h1 class="bb-page-hero__title" data-decode="Simple pricing">Simple pricing</h1>' +
      '<p class="bb-page-hero__desc">One plan.</p></div></header>',
  },
  {
    // Title only: both adapters have to DROP the eyebrow and the description
    // rather than render empty boxes, and Astro's `&&` and Svelte's `{#if}`
    // are not the same rule for an empty string.
    name: 'PageHero: title only',
    svelte: SveltePageHero,
    astro: AstroPageHero,
    props: { title: 'Contact' },
    html:
      '<header class="bb-page-hero">' +
      LIGHT_FIELD +
      '<div class="bb-page-hero__glow" aria-hidden="true"></div>' +
      '<div class="bb-page-hero__inner">' +
      '<h1 class="bb-page-hero__title" data-decode="Contact">Contact</h1></div></header>',
  },
  {
    name: 'SectionHeading: left with eyebrow',
    svelte: SvelteSectionHeading,
    astro: AstroSectionHeading,
    props: { eyebrow: 'Safety', title: 'Layers', align: 'left' },
    html:
      '<div class="bb-section-heading bb-section-heading--left">' +
      '<div class="bb-section-heading__meta" data-reveal>' +
      '<span class="bb-section-heading__eyebrow">Safety</span></div>' +
      '<h2 class="bb-section-heading__title" data-reveal style="--reveal-i: 1">Layers</h2></div>',
  },
  {
    // No eyebrow and no badge: the meta row disappears entirely, so the title
    // is the block's only child and the 14px gap goes with it.
    name: 'SectionHeading: bare',
    svelte: SvelteSectionHeading,
    astro: AstroSectionHeading,
    props: { title: 'Games' },
    html:
      '<div class="bb-section-heading bb-section-heading--center">' +
      '<h2 class="bb-section-heading__title" data-reveal style="--reveal-i: 1">Games</h2></div>',
  },
  {
    // Two glyphs is enough to prove the per-glyph indices are emitted in the
    // same order on both rows; a longer label only makes the diff harder to
    // read when it fails.
    name: 'TextLink: default',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: '/docs', label: 'Go' },
    html:
      '<a class="bb-text-link" href="/docs" aria-label="Go">' +
      '<span class="bb-text-link__mask" aria-hidden="true">' +
      '<span class="bb-text-link__row bb-text-link__row--rest">' +
      '<span class="bb-text-link__glyph" style="--gi: 0;">G</span>' +
      '<span class="bb-text-link__glyph" style="--gi: 1;">o</span></span>' +
      '<span class="bb-text-link__row bb-text-link__row--over">' +
      '<span class="bb-text-link__glyph" style="--gi: 0;">G</span>' +
      '<span class="bb-text-link__glyph" style="--gi: 1;">o</span></span></span></a>',
  },
  {
    name: 'TextLink: active external sized',
    svelte: SvelteTextLink,
    astro: AstroTextLink,
    props: { href: 'https://example.test', label: 'X', active: true, external: true, size: '1rem' },
    html:
      '<a class="bb-text-link is-active" href="https://example.test" aria-label="X" ' +
      'aria-current="page" target="_blank" rel="noopener noreferrer" style="--text-link-size: 1rem;">' +
      '<span class="bb-text-link__mask" aria-hidden="true">' +
      '<span class="bb-text-link__row bb-text-link__row--rest">' +
      '<span class="bb-text-link__glyph" style="--gi: 0;">X</span></span>' +
      '<span class="bb-text-link__row bb-text-link__row--over">' +
      '<span class="bb-text-link__glyph" style="--gi: 0;">X</span></span></span></a>',
  },
  {
    name: 'Brackets: page',
    svelte: SvelteBrackets,
    astro: AstroBrackets,
    props: {},
    html:
      '<div class="bb-ornaments bb-ornaments--page" aria-hidden="true">' +
      '<div class="bb-corner bb-corner--bl"></div>' +
      '<div class="bb-corner bb-corner--br"></div>' +
      '<div class="bb-ornament-label"></div></div>',
  },
  {
    // The loader variant defaults its own label, which is the one place the
    // two adapters compute a default rather than pass one through.
    name: 'Brackets: loader',
    svelte: SvelteBrackets,
    astro: AstroBrackets,
    props: { variant: 'loader' },
    html:
      '<div class="bb-ornaments bb-ornaments--loader" aria-hidden="true">' +
      '<div class="bb-corner bb-corner--bl"></div>' +
      '<div class="bb-corner bb-corner--br"></div>' +
      '<div class="bb-ornament-label">ItsBagelBot</div></div>',
  },
  {
    name: 'ReadingProgress: default',
    svelte: SvelteReadingProgress,
    astro: AstroReadingProgress,
    props: {},
    html:
      '<div class="bb-reading-progress" aria-hidden="true">' +
      '<span class="bb-reading-progress__fill" data-reading-progress></span></div>',
  },
];

for (const testCase of REMAINING) {
  test(`svelte adapter emits the contract markup: ${testCase.name}`, () => {
    // The two `any` casts are the price of ONE table for thirteen components
    // with thirteen different prop types. `render` is generic over its
    // component, so a heterogeneous array cannot be typed without a union that
    // would have to be updated by hand on every row — which is the maintenance
    // this table exists to avoid. The assertion below is what actually checks
    // the props: a wrong one produces wrong markup.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const { body } = render(testCase.svelte as any, { props: testCase.props as any });
    expect(normalise(body)).toBe(testCase.html);
  });

  test(`astro adapter emits the contract markup: ${testCase.name}`, async () => {
    const container = await experimental_AstroContainer.create();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const html = await container.renderToString(testCase.astro as any, {
      props: testCase.props,
    });
    expect(normalise(html)).toBe(testCase.html);
  });
}

/**
 * The Svelte-only halves. Each is here because the element genuinely has no
 * Astro equivalent for that case, not because writing the twin was awkward.
 */

test('Modal: a title mints an id and points aria-labelledby at it', () => {
  // $props.id() is per-render, so the id itself is not asserted — only that
  // the two ends agree, which is the whole job of the pair.
  const { body } = render(SvelteModal, {
    props: { open: true, title: 'Delete timer' },
  });
  const html = normalise(body);
  const id = /<h3 class="bb-modal__title" id="([^"]+)">/.exec(html)?.[1];
  expect(id).toBeTruthy();
  expect(html).toContain(`aria-labelledby="${id}"`);
  expect(html).toContain('>Delete timer</h3>');
});

test('Modal: closed renders nothing at all', () => {
  // Not "renders hidden": a modal left in the tree would keep the overlay
  // stack's inert sweep excluding it and leave a focusable backdrop button in
  // the tab order behind the page.
  const { body } = render(SvelteModal, { props: { open: false } });
  expect(normalise(body)).toBe('');
});

test('SaveStatus: idle renders nothing', () => {
  // The row has no indicator until something has happened to it. An empty tag
  // would still take its gap in the row's flex layout.
  const { body } = render(SvelteSaveStatus, { props: { state: 'idle' } });
  expect(normalise(body)).toBe('');
});

test('AreaSeries: fewer than two points draws no path', async () => {
  const SvelteAreaSeries = (await import('../svelte/AreaSeries.svelte')).default;
  // A one-point series has no line to draw and no scale to draw it against.
  // It must render the empty chrome rather than a NaN path, which browsers
  // report only as a silent non-render.
  const { body } = render(SvelteAreaSeries, {
    props: { values: [4], ariaLabel: 'Chat volume' },
  });
  const html = normalise(body);
  expect(html).toContain('class="bb-area"');
  expect(html).not.toContain('NaN');
  expect(html).not.toContain('class="bb-area__dot"');
});

test('ErrorScene: carries no copy of its own', async () => {
  const SvelteErrorScene = (await import('../svelte/ErrorScene.svelte')).default;
  // The element is the scene; every sentence arrives as a prop. This is the
  // test that fails the day someone moves a default string back into it,
  // which is how a design library stops being one.
  const { body } = render(SvelteErrorScene, {
    props: { status: 404, eyebrow: 'E', title: 'T', description: 'D' },
  });
  const html = normalise(body);
  expect(html).toContain('<h1 class="bb-error-scene__title" id="bb-error-title">T</h1>');
  expect(html).not.toContain('bagel');
  expect(html).not.toContain('Bagel');
});
