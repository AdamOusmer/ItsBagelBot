// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
  const { body } = render(SvelteCursor, { props: { enabled: false } });
  expect(normalise(body)).toBe('');
});

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

const NAV_LINK_HTML =
  '<a class="bb-nav-link" href="/pricing">' +
  '<span class="bb-nav-link__label">Pricing</span></a>';

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

import { describe } from 'bun:test';
import SvelteButton from './fixtures/button.svelte';
import AstroButton from './fixtures/button.astro';
import SvelteCard from './fixtures/card.svelte';
import AstroCard from './fixtures/card.astro';
import SvelteCardHead from './fixtures/cardhead.svelte';
import AstroCardHead from './fixtures/cardhead.astro';

/* eslint-disable @typescript-eslint/no-explicit-any */
type Adapter = any;

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

interface Case {
  name: string;
  svelte: Adapter;
  astro: Adapter;
  props: Record<string, unknown>;
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

  contract({
    name: 'icon-only carries no mark and an author-supplied name',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'icon', icon: true, label: 'Close' },
    html: '<button class="bb-btn bb-btn--icon" type="button" data-mark aria-label="Close"><span class="bb-btn__content"><svg viewBox="0 0 24 24"></svg></span></button>',
  });

  contract({
    name: 'compact destructive icon preserves its accessible name and glyph',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'icon', size: 'sm', danger: true, icon: true, label: 'Delete' },
    html: '<button class="bb-btn bb-btn--icon bb-btn--danger-hover bb-btn--sm" type="button" data-mark aria-label="Delete"><span class="bb-btn__content"><svg viewBox="0 0 24 24"></svg></span></button>',
  });

  contract({
    name: 'href renders an anchor (Astro Button) == ButtonLink (Svelte)',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { link: true, variant: 'secondary' },
    html: '<a class="bb-btn bb-btn--secondary" href="/x" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Save</span></a>',
  });
});

describe('Card', () => {
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

test('badge.css ships no perm ladder and no purple', async () => {
  const css = await Bun.file(new URL('../styles/elements/badge.css', import.meta.url)).text();
  const rules = css.replace(/\/\*[\s\S]*?\*\//g, '');
  expect(rules).not.toMatch(/#d9aaff/i);
  expect(rules).not.toMatch(/everyone|broadcaster|lead_mod/);
});

import { CASES } from './pr10-cases';

for (const parityCase of CASES) {
  const golden = (
    await Bun.file(
      new URL(`./__golden__/${parityCase.name}.html`, import.meta.url).pathname,
    ).text()
  ).trim();

  test(`${parityCase.name}: svelte adapter matches the golden`, () => {
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
    name: 'ManagementRow: selected',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { selected: true, expanded: true, controls: 'insp', disabled: true, accent: true },
    html:
      '<div class="bb-row row-shell bb-row--accent is-selected is-off">' +
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
    name: 'SectionHeading: bare',
    svelte: SvelteSectionHeading,
    astro: AstroSectionHeading,
    props: { title: 'Games' },
    html:
      '<div class="bb-section-heading bb-section-heading--center">' +
      '<h2 class="bb-section-heading__title" data-reveal style="--reveal-i: 1">Games</h2></div>',
  },
  {
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

test('Modal: a title mints an id and points aria-labelledby at it', () => {
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
  const { body } = render(SvelteModal, { props: { open: false } });
  expect(normalise(body)).toBe('');
});

test('SaveStatus: idle renders nothing', () => {
  const { body } = render(SvelteSaveStatus, { props: { state: 'idle' } });
  expect(normalise(body)).toBe('');
});

test('AreaSeries: fewer than two points draws no path', async () => {
  const SvelteAreaSeries = (await import('../svelte/AreaSeries.svelte')).default;
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
  const { body } = render(SvelteErrorScene, {
    props: { status: 404, eyebrow: 'E', title: 'T', description: 'D' },
  });
  const html = normalise(body);
  expect(html).toContain('<h1 class="bb-error-scene__title" id="bb-error-title">T</h1>');
  expect(html).not.toContain('bagel');
  expect(html).not.toContain('Bagel');
});

import SvelteHeading from '../svelte/Heading.svelte';
import AstroHeading from '../astro/Heading.astro';
import SvelteText from '../svelte/Text.svelte';
import AstroText from '../astro/Text.astro';
import SvelteEyebrow from '../svelte/Eyebrow.svelte';
import AstroEyebrow from '../astro/Eyebrow.astro';
import SvelteLead from '../svelte/Lead.svelte';
import AstroLead from '../astro/Lead.astro';
import SvelteKbd from '../svelte/Kbd.svelte';
import AstroKbd from '../astro/Kbd.astro';
import SvelteCode from '../svelte/Code.svelte';
import AstroCode from '../astro/Code.astro';
import SvelteLabel from '../svelte/Label.svelte';
import AstroLabel from '../astro/Label.astro';
import SvelteVisuallyHidden from '../svelte/VisuallyHidden.svelte';
import AstroVisuallyHidden from '../astro/VisuallyHidden.astro';
import SvelteContainer from '../svelte/Container.svelte';
import AstroContainer from '../astro/Container.astro';
import SvelteSection from '../svelte/Section.svelte';
import AstroSection from '../astro/Section.astro';
import SvelteStack from '../svelte/Stack.svelte';
import AstroStack from '../astro/Stack.astro';
import SvelteCluster from '../svelte/Cluster.svelte';
import AstroCluster from '../astro/Cluster.astro';
import SvelteGrid from '../svelte/Grid.svelte';
import AstroGrid from '../astro/Grid.astro';
import SvelteDivider from '../svelte/Divider.svelte';
import AstroDivider from '../astro/Divider.astro';
import SvelteSpacer from '../svelte/Spacer.svelte';
import AstroSpacer from '../astro/Spacer.astro';
import SvelteInput from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';
import SvelteSelect from '../svelte/Select.svelte';
import AstroSelect from '../astro/Select.astro';
import SvelteTextarea from '../svelte/Textarea.svelte';
import AstroTextarea from '../astro/Textarea.astro';
import SvelteCheckbox from '../svelte/Checkbox.svelte';
import AstroCheckbox from '../astro/Checkbox.astro';
import SvelteIconButton from '../svelte/IconButton.svelte';
import AstroIconButton from '../astro/IconButton.astro';
import SvelteButtonLink from '../svelte/ButtonLink.svelte';
import AstroButtonLink from '../astro/ButtonLink.astro';
import SvelteTag from '../svelte/Tag.svelte';
import AstroTag from '../astro/Tag.astro';
import SvelteTooltip from '../svelte/Tooltip.svelte';
import AstroTooltip from '../astro/Tooltip.astro';
import SvelteTable from '../svelte/Table.svelte';
import AstroTable from '../astro/Table.astro';
import SvelteSkeleton from '../svelte/Skeleton.svelte';
import AstroSkeleton from '../astro/Skeleton.astro';
import SvelteSkeletonStack from '../svelte/SkeletonStack.svelte';
import AstroSkeletonStack from '../astro/SkeletonStack.astro';
import SvelteSegmentedControl from '../svelte/SegmentedControl.svelte';
import AstroSegmentedControl from '../astro/SegmentedControl.astro';
import SvelteRadioGroup from '../svelte/RadioGroup.svelte';
import AstroRadioGroup from '../astro/RadioGroup.astro';
import SvelteSectionNav from '../svelte/SectionNav.svelte';
import AstroSectionNav from '../astro/SectionNav.astro';
import SvelteScroller from '../svelte/Scroller.svelte';
import AstroScroller from '../astro/Scroller.astro';
import SvelteBackgroundOrbs from '../svelte/BackgroundOrbs.svelte';
import AstroBackgroundOrbs from '../astro/BackgroundOrbs.astro';
import SvelteAuroraBg from '../svelte/AuroraBg.svelte';
import AstroAuroraBg from '../astro/AuroraBg.astro';
import SvelteAreaSeries from '../svelte/AreaSeries.svelte';
import AstroAreaSeries from '../astro/AreaSeries.astro';
import SvelteNavGroup from '../svelte/NavGroup.svelte';
import AstroNavGroup from '../astro/NavGroup.astro';
import SvelteErrorScene from '../svelte/ErrorScene.svelte';
import AstroErrorScene from '../astro/ErrorScene.astro';

const PRIMITIVES: {
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
  slot?: string;
  html: string;
}[] = [
{
    name: "Heading",
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: {"level":1,"variant":"display"},
    slot: "Ship it",
    html: "<h1 class=\"bb-h bb-h--l1 bb-h--display\">Ship it</h1>",
  },
  {
    name: "Heading|card",
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: {"level":4,"variant":"card"},
    slot: "Recent",
    html: "<h4 class=\"bb-h bb-h--l4 bb-h--card\">Recent</h4>",
  },
  {
    name: "Heading|as",
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: {"level":2,"as":"p"},
    slot: "Not in the outline",
    html: "<p class=\"bb-h bb-h--l2\">Not in the outline</p>",
  },
  {
    name: "Text",
    svelte: SvelteText,
    astro: AstroText,
    props: {},
    slot: "Body copy.",
    html: "<p class=\"bb-text bb-text--md\">Body copy.</p>",
  },
  {
    name: "Text|toned",
    svelte: SvelteText,
    astro: AstroText,
    props: {"size":"sm","tone":"muted","mono":true,"as":"span"},
    slot: "12 ms",
    html: "<span class=\"bb-text bb-text--sm bb-text--muted bb-text--mono\">12 ms</span>",
  },
  {
    name: "Eyebrow",
    svelte: SvelteEyebrow,
    astro: AstroEyebrow,
    props: {},
    slot: "Live",
    html: "<span class=\"bb-eyebrow\">Live</span>",
  },
  {
    name: "Lead",
    svelte: SvelteLead,
    astro: AstroLead,
    props: {},
    slot: "One paragraph under the hero.",
    html: "<p class=\"bb-lead\">One paragraph under the hero.</p>",
  },
  {
    name: "Kbd",
    svelte: SvelteKbd,
    astro: AstroKbd,
    props: {},
    slot: "K",
    html: "<kbd class=\"bb-kbd\">K</kbd>",
  },
  {
    name: "Code",
    svelte: SvelteCode,
    astro: AstroCode,
    props: {},
    slot: "bun run check",
    html: "<code class=\"bb-code\">bun run check</code>",
  },
  {
    name: "Code|danger",
    svelte: SvelteCode,
    astro: AstroCode,
    props: {"tone":"danger"},
    slot: "!uptime",
    html: "<code class=\"bb-code bb-code--danger\">!uptime</code>",
  },
  {
    name: "Code|block",
    svelte: SvelteCode,
    astro: AstroCode,
    props: {"block":true},
    slot: "bun test",
    html: "<pre class=\"bb-code-block\"><code class=\"bb-code\">bun test</code></pre>",
  },
  {
    name: "Label",
    svelte: SvelteLabel,
    astro: AstroLabel,
    props: {"htmlFor":"cooldown"},
    slot: "Cooldown",
    html: "<label class=\"bb-label\" for=\"cooldown\">Cooldown</label>",
  },
  {
    name: "VisuallyHidden",
    svelte: SvelteVisuallyHidden,
    astro: AstroVisuallyHidden,
    props: {"focusable":true,"as":"a"},
    slot: "Skip to content",
    html: "<a class=\"bb-sr-only bb-sr-only--focusable\">Skip to content</a>",
  },
  {
    name: "Container",
    svelte: SvelteContainer,
    astro: AstroContainer,
    props: {"width":"narrow","flush":true},
    slot: "x",
    html: "<div class=\"bb-container bb-container--narrow bb-container--flush\">x</div>",
  },
  {
    name: "Section",
    svelte: SvelteSection,
    astro: AstroSection,
    props: {"size":"lg","anchor":true,"reveal":true},
    slot: "x",
    html: "<section class=\"bb-section bb-section--lg bb-section--anchor\" data-reveal>x</section>",
  },
  {
    name: "Stack",
    svelte: SvelteStack,
    astro: AstroStack,
    props: {"gap":6,"align":"center"},
    slot: "x",
    html: "<div class=\"bb-stack bb-stack--6 bb-stack--center\">x</div>",
  },
  {
    name: "Cluster",
    svelte: SvelteCluster,
    astro: AstroCluster,
    props: {"gap":3,"justify":"between","nowrap":true},
    slot: "x",
    html: "<div class=\"bb-cluster bb-cluster--3 bb-cluster--between bb-cluster--nowrap\">x</div>",
  },
  {
    name: "Grid",
    svelte: SvelteGrid,
    astro: AstroGrid,
    props: {"min":"240px","gap":5},
    slot: "x",
    html: "<div class=\"bb-grid bb-grid--auto bb-grid--gap-5\" style=\"--grid-min: 240px;\">x</div>",
  },
  {
    name: "Grid|fixed",
    svelte: SvelteGrid,
    astro: AstroGrid,
    props: {"cols":3},
    slot: "x",
    html: "<div class=\"bb-grid bb-grid--3 bb-grid--gap-4\">x</div>",
  },
  {
    name: "Divider",
    svelte: SvelteDivider,
    astro: AstroDivider,
    props: {},
    html: "<hr class=\"bb-divider\">",
  },
  {
    name: "Divider|vertical",
    svelte: SvelteDivider,
    astro: AstroDivider,
    props: {"vertical":true,"fade":true},
    html: "<span class=\"bb-divider bb-divider--v bb-divider--fade\" aria-hidden=\"true\"></span>",
  },
  {
    name: "Spacer",
    svelte: SvelteSpacer,
    astro: AstroSpacer,
    props: {"grow":true},
    html: "<span class=\"bb-spacer bb-spacer--grow\" aria-hidden=\"true\"></span>",
  },
  {
    name: "Input",
    svelte: SvelteInput,
    astro: AstroInput,
    props: {"type":"email","invalid":true,"name":"email","placeholder":"you@example.com"},
    html: "<span class=\"bb-input\" data-invalid><input type=\"email\" value name=\"email\" placeholder=\"you@example.com\"></span>",
  },
  {
    name: "Select",
    svelte: SvelteSelect,
    astro: AstroSelect,
    props: {"name":"mode"},
    slot: "<option value=\"a\">A</option>",
    html: "<span class=\"bb-input bb-input--select\"><select name=\"mode\"><option value=\"a\">A</option></select><svg class=\"bb-input__chevron\" viewBox=\"0 0 24 24\" aria-hidden=\"true\"><path d=\"m6 9 6 6 6-6\"></path></svg></span>",
  },
  {
    name: "Textarea",
    svelte: SvelteTextarea,
    astro: AstroTextarea,
    props: {"rows":4,"name":"body","value":"hi"},
    html: "<span class=\"bb-input bb-input--area\"><textarea rows=\"4\" name=\"body\">hi</textarea></span>",
  },
  {
    name: "Checkbox",
    svelte: SvelteCheckbox,
    astro: AstroCheckbox,
    props: {"checked":true,"name":"optin"},
    slot: "Email me",
    html: "<label class=\"bb-check\"><input type=\"checkbox\" class=\"bb-check__input\" checked name=\"optin\"><span class=\"bb-check__box\" aria-hidden=\"true\"></span><span class=\"bb-check__label\">Email me</span></label>",
  },
  {
    name: "IconButton",
    svelte: SvelteIconButton,
    astro: AstroIconButton,
    props: {"label":"Close","tooltip":true},
    slot: "<svg></svg>",
    html: "<span class=\"bb-tooltip\"><button class=\"bb-btn bb-btn--icon\" type=\"button\" aria-label=\"Close\" data-mark><span class=\"bb-btn__content\"><svg></svg></span></button><span class=\"bb-tooltip__bubble\" aria-hidden=\"true\">Close</span></span>",
  },
  {
    name: "IconButton|plain",
    svelte: SvelteIconButton,
    astro: AstroIconButton,
    props: {"label":"Close","size":"sm"},
    slot: "<svg></svg>",
    html: "<button class=\"bb-btn bb-btn--icon bb-btn--sm\" type=\"button\" aria-label=\"Close\" data-mark><span class=\"bb-btn__content\"><svg></svg></span></button>",
  },
  {
    name: "ButtonLink",
    svelte: SvelteButtonLink,
    astro: AstroButtonLink,
    props: {"href":"/pricing","variant":"green"},
    slot: "Go",
    html: "<a class=\"bb-btn bb-btn--green\" href=\"/pricing\" data-mark><i class=\"bb-btn__mark\" aria-hidden=\"true\"></i><span class=\"bb-btn__content\">Go</span></a>",
  },
  {
    name: "Tag",
    svelte: SvelteTag,
    astro: AstroTag,
    props: {"tone":"live","mark":"solid","sweep":true,"status":true},
    slot: "Live",
    html: "<span class=\"bb-tag bb-tag--live\" role=\"status\"><i class=\"bb-mark\" aria-hidden=\"true\"></i>Live<i class=\"bb-sweep\" aria-hidden=\"true\"></i></span>",
  },
  {
    name: "Tooltip",
    svelte: SvelteTooltip,
    astro: AstroTooltip,
    props: {"text":"Copy","id":"tt-1"},
    slot: "<button></button>",
    html: "<span class=\"bb-tooltip\"><button></button><span class=\"bb-tooltip__bubble\" id=\"tt-1\" role=\"tooltip\">Copy</span></span>",
  },
  {
    name: "Table",
    svelte: SvelteTable,
    astro: AstroTable,
    props: {"label":"Counters","zebra":true},
    slot: "<tbody><tr><td>1</td></tr></tbody>",
    html: "<div class=\"bb-tbl-wrap\" role=\"region\" aria-label=\"Counters\" tabindex=\"0\"><table class=\"bb-tbl bb-tbl--zebra\"><tbody><tr><td>1</td></tr></tbody></table></div>",
  },
  {
    name: "Skeleton",
    svelte: SvelteSkeleton,
    astro: AstroSkeleton,
    props: {"variant":"text","lines":3},
    html: "<span class=\"bb-skel-lines\" style=\"--skel-w:100%;\"><span class=\"bb-skel bb-skel--text\" style=\"--skel-w:100%;\"></span><span class=\"bb-skel bb-skel--text\" style=\"--skel-w:100%;\"></span><span class=\"bb-skel bb-skel--text\" style=\"--skel-w:60%;\"></span></span>",
  },
  {
    name: "Skeleton|block",
    svelte: SvelteSkeleton,
    astro: AstroSkeleton,
    props: {"variant":"block","height":"260px"},
    html: "<span class=\"bb-skel bb-skel--block\" style=\"--skel-h:260px;\"></span>",
  },
  {
    name: "SkeletonStack",
    svelte: SvelteSkeletonStack,
    astro: AstroSkeletonStack,
    props: {"rows":2,"height":"80px","columns":2},
    html: "<div class=\"bb-skel-stack bb-skel-stack--grid\" aria-hidden=\"true\"><span class=\"bb-skel bb-skel--block\" style=\"--skel-h:80px;\"></span><span class=\"bb-skel bb-skel--block\" style=\"--skel-h:80px;\"></span></div>",
  },
  {
    name: "SegmentedControl",
    svelte: SvelteSegmentedControl,
    astro: AstroSegmentedControl,
    props: {"options":["All","Live"],"value":"Live"},
    html: "<div class=\"bb-tabs bb-tabs--wrap\" role=\"radiogroup\" aria-label=\"Filter\"><button type=\"button\" class=\"bb-tab \" role=\"radio\" aria-checked=\"false\">All</button><button type=\"button\" class=\"bb-tab is-active\" role=\"radio\" aria-checked=\"true\">Live</button></div>",
  },
  {
    name: "RadioGroup",
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: {"name":"tier","options":[{"value":"a","label":"A"}],"value":"a"},
    html: "<div class=\"bb-tabs bb-tabs--wrap\" role=\"radiogroup\" aria-label=\"Options\"><label class=\"bb-tab is-active\"><input class=\"bb-tab__input\" type=\"radio\" name=\"tier\" value=\"a\" checked> A</label></div>",
  },
  {
    name: "SectionNav",
    svelte: SvelteSectionNav,
    astro: AstroSectionNav,
    props: {"label":"Sections","items":[{"href":"#a","label":"A","count":2}]},
    html: "<div class=\"bb-tabs-host\"><nav class=\"bb-tabs bb-tabs--auto\" aria-label=\"Sections\"><a class=\"bb-tab\" href=\"#a\">A<span class=\"bb-tab__count\">2</span></a></nav></div>",
  },
  {
    name: "SectionNav|vertical",
    svelte: SvelteSectionNav,
    astro: AstroSectionNav,
    props: {"label":"Sections","items":[{"href":"#a","label":"A"}],"orientation":"vertical"},
    html: "<div class=\"bb-tabs-host\"><nav class=\"bb-tabs bb-tabs--vertical\" aria-label=\"Sections\"><a class=\"bb-tab\" href=\"#a\">A</a></nav></div>",
  },
  {
    name: "Scroller",
    svelte: SvelteScroller,
    astro: AstroScroller,
    props: {"maxHeight":"208px","fill":true},
    slot: "x",
    html: "<div class=\"bb-scroller bb-scroller--fill bb-scroll\" style=\"max-height:208px\">x</div>",
  },
  {
    name: "BackgroundOrbs",
    svelte: SvelteBackgroundOrbs,
    astro: AstroBackgroundOrbs,
    props: {},
    html: "<div class=\"bb-orb bb-orb--fixed bb-orb--wash-green bb-bg-orb\"></div><div class=\"bb-orb bb-orb--fixed bb-orb--wash-tan bb-bg-orb bb-bg-orb--two\"></div>",
  },
  {
    name: "AuroraBg",
    svelte: SvelteAuroraBg,
    astro: AstroAuroraBg,
    props: {},
    html: "<div class=\"bb-aurora\" aria-hidden=\"true\"><span class=\"bb-orb bb-orb--aurora-green bb-orb--drift-1 bb-aurora__o1\"></span><span class=\"bb-orb bb-orb--aurora-tan bb-orb--drift-2 bb-aurora__o2\"></span><span class=\"bb-orb bb-orb--aurora-green-soft bb-orb--drift-3 bb-aurora__o3\"></span></div>",
  },
  {
    name: "AreaSeries",
    svelte: SvelteAreaSeries,
    astro: AstroAreaSeries,
    props: {"values":[1,4,2],"ariaLabel":"Chat volume","uid":"p","ticks":[1]},
    html: "<svg class=\"bb-area\" style=\"height: 178px\" viewBox=\"0 0 640 178\" preserveAspectRatio=\"none\" role=\"img\" aria-label=\"Chat volume\"><defs><linearGradient id=\"bb-area-fill-p\" x1=\"0\" y1=\"0\" x2=\"0\" y2=\"1\"><stop offset=\"0%\" stop-color=\"var(--bb-green-glow)\" stop-opacity=\"0.3\"></stop><stop offset=\"100%\" stop-color=\"var(--bb-green-glow)\" stop-opacity=\"0.02\"></stop></linearGradient></defs><line x1=\"0\" y1=\"44\" x2=\"640\" y2=\"44\" class=\"bb-area__grid\"></line><line x1=\"0\" y1=\"94\" x2=\"640\" y2=\"94\" class=\"bb-area__grid\"></line><line x1=\"0\" y1=\"144\" x2=\"640\" y2=\"144\" class=\"bb-area__grid bb-area__grid--base\"></line><path d=\"M0,111 L320,12 L640,78 L640,144 L0,144 Z\" fill=\"url(#bb-area-fill-p)\"></path><path d=\"M0,111 L320,12 L640,78\" class=\"bb-area__line\"></path><g class=\"bb-area__ticks\"><line x1=\"320\" y1=\"150\" x2=\"320\" y2=\"160\"></line></g><circle cx=\"640\" cy=\"78\" r=\"3.5\" class=\"bb-area__dot\"></circle></svg>",
  },
  {
    name: "NavGroup",
    svelte: SvelteNavGroup,
    astro: AstroNavGroup,
    props: {"label":"Boards","items":[{"href":"/a","label":"A","count":3}]},
    html: "<div class=\"bb-nav-group__label\">Boards</div><nav class=\"bb-nav-group\"><a class=\"bb-nav-link bb-nav-link--block\" href=\"/a\"><span class=\"bb-nav-group__index\">01</span><span class=\"bb-nav-link__label\">A</span><span class=\"bb-nav-group__count\">3</span></a></nav>",
  },
  {
    name: "ErrorScene",
    svelte: SvelteErrorScene,
    astro: AstroErrorScene,
    props: {"status":404,"eyebrow":"Not found","title":"T","description":"D","aside":"A"},
    html: "<main class=\"bb-error-scene\" aria-labelledby=\"bb-error-title\"><canvas class=\"bb-light-field\" data-field data-warmth=\"0.7\" aria-hidden=\"true\"></canvas><div class=\"bb-error-scene__glow\" aria-hidden=\"true\"></div><div class=\"bb-error-scene__orbits\" aria-hidden=\"true\"><span class=\"bb-error-scene__orbit\"></span><span class=\"bb-error-scene__orbit bb-error-scene__orbit--two\"></span></div><div class=\"bb-error-scene__content\"><p class=\"bb-error-scene__eyebrow\"><span>404</span> · Not found</p><p class=\"bb-error-scene__code\" aria-hidden=\"true\">404</p><h1 class=\"bb-error-scene__title\" id=\"bb-error-title\">T</h1><p class=\"bb-error-scene__desc\">D</p><p class=\"bb-error-scene__aside\">A</p></div></main>",
  },
];

for (const primitive of PRIMITIVES) {
  test(`${primitive.name}: svelte adapter emits the contract markup`, () => {
    const props = { ...primitive.props } as Record<string, unknown>;
    if (primitive.slot !== undefined) {
      props.children = createRawSnippet(() => ({ render: () => primitive.slot as string }));
    }
    const { body } = render(primitive.svelte as never, { props: props as never });
    expect(normalise(body)).toBe(primitive.html);
  });

  test(`${primitive.name}: astro adapter emits the contract markup`, async () => {
    const container = await experimental_AstroContainer.create();
    const html = await container.renderToString(primitive.astro as never, {
      props: primitive.props as never,
      ...(primitive.slot === undefined ? {} : { slots: { default: primitive.slot } }),
    });
    expect(normalise(html)).toBe(primitive.html);
  });
}

test('typography.css keeps the bare-element rules in bb.base', async () => {
  const css = await Bun.file(
    new URL('../styles/elements/typography.css', import.meta.url),
  ).text();
  const body = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const base = body.slice(body.indexOf('@layer bb.base'));
  for (const selector of ['h1 {', 'p {', 'small {', 'code, pre, kbd, samp {']) {
    expect(body).toContain(selector);
    expect(base).toContain(selector);
  }
  const elements = body.slice(body.indexOf('@layer bb.elements'), body.indexOf('@layer bb.base'));
  expect(elements).not.toMatch(/^\s{4}(h[1-6]|p|small|code|kbd)\s*[,{]/m);
});

test('magnetic keeps its tuned ease and settle threshold', async () => {
  const src = await Bun.file(new URL('../lib/magnetic.ts', import.meta.url)).text();
  expect(src).toContain('const EASE = 0.2;');
  expect(src).toContain('const SETTLE_PX = 0.1;');
});
