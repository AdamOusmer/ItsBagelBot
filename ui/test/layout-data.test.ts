// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import SvelteCard from '../svelte/Card.svelte';
import AstroCard from '../astro/Card.astro';
import SvelteDeckLayout from '../svelte/DeckLayout.svelte';
import AstroDeckLayout from '../astro/DeckLayout.astro';
import SvelteDisclosure from '../svelte/Disclosure.svelte';
import AstroDisclosure from '../astro/Disclosure.astro';
import SvelteFact from '../svelte/Fact.svelte';
import AstroFact from '../astro/Fact.astro';
import SvelteFactList from '../svelte/FactList.svelte';
import AstroFactList from '../astro/FactList.astro';
import SvelteManagementRow from '../svelte/ManagementRow.svelte';
import AstroManagementRow from '../astro/ManagementRow.astro';
import SveltePager from '../svelte/Pager.svelte';
import AstroPager from '../astro/Pager.astro';
import SvelteProgressBar from '../svelte/ProgressBar.svelte';
import AstroProgressBar from '../astro/ProgressBar.astro';
import SvelteStatTile from '../svelte/StatTile.svelte';
import AstroStatTile from '../astro/StatTile.astro';

type Adapter = never;

interface Case {
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
  slots?: Record<string, string>;
  defaultSnippet?: string;
  html: string;
}

function snippetsFor(slots: Record<string, string>, defaultSnippet: string) {
  const snippets: Record<string, unknown> = {};
  for (const [name, html] of Object.entries(slots)) {
    snippets[name === 'default' ? defaultSnippet : name] = createRawSnippet(() => ({ render: () => html }));
  }
  return snippets;
}

function contract({ name, svelte, astro, props, slots = {}, defaultSnippet = 'children', html }: Case) {
  test(`${name}: svelte`, () => {
    const all = { ...props, ...snippetsFor(slots, defaultSnippet) };
    expect(normalise(render(svelte as Adapter, { props: all as never }).body)).toBe(html);
  });

  test(`${name}: astro`, async () => {
    const container = await experimental_AstroContainer.create();
    const out = await container.renderToString(astro as Adapter, { props, slots });
    expect(normalise(out)).toBe(html);
  });
}

describe('DeckLayout', () => {
  contract({
    name: 'idle deck is one column',
    svelte: SvelteDeckLayout,
    astro: AstroDeckLayout,
    props: {},
    slots: { default: '<p>list</p>' },
    html: '<div class="bb-deck-layout"><p>list</p></div>',
  });

  contract({
    name: 'inspecting with the admin column width',
    svelte: SvelteDeckLayout,
    astro: AstroDeckLayout,
    props: { inspecting: true, width: '380px', class: 'users' },
    slots: { default: '<p>list</p><aside>inspector</aside>' },
    html:
      '<div class="bb-deck-layout is-inspecting users" style="--deck-aside: 380px;">' +
      '<p>list</p><aside>inspector</aside></div>',
  });
});

describe('ManagementRow modes', () => {
  contract({
    name: 'href renders an anchor primary',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { href: '/modules/quotes', label: 'Open Quotes', selected: true },
    slots: { default: 'Quotes' },
    defaultSnippet: 'primary',
    html:
      '<div class="bb-row row-shell is-selected">' +
      '<a class="bb-row__primary" href="/modules/quotes" data-cursor="quiet" aria-current="true" aria-label="Open Quotes">Quotes</a>' +
      '</div>',
  });

  contract({
    name: 'non-selectable row is static',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { selectable: false, as: 'article' },
    slots: { default: 'shard 0', actions: '<i>m</i>' },
    defaultSnippet: 'primary',
    html:
      '<article class="bb-row row-shell bb-row--static">' +
      '<div class="bb-row__primary" data-cursor="quiet">shard 0</div>' +
      '<div class="bb-row__actions"><i>m</i></div></article>',
  });

  contract({
    name: 'title, meta and marks render the shared row line',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { title: 'ada', meta: 'id 4', controls: 'insp' },
    slots: { leading: '<b>L</b>', badge: '<b>You</b>', default: '<b>meter</b>', marks: '<b>paid</b>' },
    defaultSnippet: 'primary',
    html:
      '<div class="bb-row row-shell">' +
      '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="false" aria-controls="insp">' +
      '<span class="bb-row__line"><b>L</b><span class="bb-row__text">' +
      '<span class="bb-row__title">ada<span class="bb-row__badge"><b>You</b></span></span>' +
      '<span class="bb-row__meta">id 4</span></span><b>meter</b>' +
      '<span class="bb-row__marks"><b>paid</b></span></span></button></div>',
  });

  contract({
    name: 'title alone renders no empty slots',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { title: 'lane' },
    html:
      '<div class="bb-row row-shell">' +
      '<button class="bb-row__primary" type="button" data-cursor="quiet" aria-expanded="false">' +
      '<span class="bb-row__line"><span class="bb-row__text"><span class="bb-row__title">lane</span></span></span>' +
      '</button></div>',
  });

  contract({
    name: 'wrapping title and stacked actions are opt-in modifiers',
    svelte: SvelteManagementRow,
    astro: AstroManagementRow,
    props: { selectable: false, wrap: true, stackActions: true, title: 'Ada Lovelace' },
    slots: { badge: '<b>You</b>', actions: '<i>go</i>' },
    html:
      '<div class="bb-row row-shell bb-row--static bb-row--wrap bb-row--stack-actions">' +
      '<div class="bb-row__primary" data-cursor="quiet"><span class="bb-row__line"><span class="bb-row__text">' +
      '<span class="bb-row__title">Ada Lovelace<span class="bb-row__badge"><b>You</b></span></span></span></span></div>' +
      '<div class="bb-row__actions"><i>go</i></div></div>',
  });

  test('wrap lets the title break beside its badge, and stacked actions indent on phones', async () => {
    const css = await Bun.file(new URL('../styles/elements/management-row.css', import.meta.url)).text();
    expect(css).toMatch(/\.bb-row--wrap \.bb-row__title \{[^}]*display: flex;[^}]*overflow-wrap: anywhere;[^}]*white-space: normal;/);
    expect(css).toMatch(/\.bb-row--wrap \.bb-row__badge \{\s*flex: none;/);
    expect(css).toMatch(
      /@media \(max-width: 600px\) \{\s*\.bb-row--stack-actions \{\s*flex-wrap: wrap;\s*\}\s*\.bb-row--stack-actions \.bb-row__actions \{[^}]*flex-basis: 100%;[^}]*var\(--row-actions-indent, 14px\)/,
    );
  });
});

describe('DeckList and Grid', () => {
  test('a deck list drops the list margin and markers when it renders as a list', async () => {
    const css = await Bun.file(new URL('../styles/elements/deck-list.css', import.meta.url)).text();
    expect(css).toMatch(/\.bb-deck-list \{[^}]*margin: 0;[^}]*list-style: none;/);
  });

  test('stackAt md collapses fixed grids at 760px, after the column classes', async () => {
    const css = await Bun.file(new URL('../styles/elements/layout.css', import.meta.url)).text();
    expect(css).toMatch(/@media \(max-width: 760px\) \{\s*\.bb-grid--stack-md \{ --grid-cols: 1; \}/);
    expect(css.indexOf('.bb-grid--stack-md {')).toBeGreaterThan(css.indexOf('.bb-grid--6 {'));
  });
});

describe('FactList', () => {
  contract({
    name: 'rows is the default layout',
    svelte: SvelteFactList,
    astro: AstroFactList,
    props: {},
    slots: { default: '<div>f</div>' },
    html: '<dl class="bb-facts"><div>f</div></dl>',
  });

  contract({
    name: 'tiles layout',
    svelte: SvelteFactList,
    astro: AstroFactList,
    props: { layout: 'tiles', class: 'review' },
    slots: { default: '<div>f</div>' },
    html: '<dl class="bb-facts bb-facts--tiles review"><div>f</div></dl>',
  });

  contract({
    name: 'fact: plain term and value',
    svelte: SvelteFact,
    astro: AstroFact,
    props: { term: 'Stream' },
    slots: { default: 'BOTS' },
    html: '<div class="bb-fact"><dt class="bb-fact__term">Stream</dt><dd class="bb-fact__value">BOTS</dd></div>',
  });

  contract({
    name: 'fact: every modifier',
    svelte: SvelteFact,
    astro: AstroFact,
    props: { term: 'Key', tone: 'danger', mono: true, wide: true, truncate: true },
    slots: { default: 'missing' },
    html:
      '<div class="bb-fact bb-fact--danger bb-fact--mono bb-fact--wide bb-fact--truncate">' +
      '<dt class="bb-fact__term">Key</dt><dd class="bb-fact__value">missing</dd></div>',
  });
});

describe('StatTile without a delta', () => {
  contract({
    name: 'no delta row is rendered',
    svelte: SvelteStatTile,
    astro: AstroStatTile,
    props: { label: 'Eligible', value: '42' },
    html:
      '<div class="bb-stat"><div class="bb-stat__head"><span class="bb-stat__label">Eligible</span></div>' +
      '<div class="bb-stat__value"><span data-count-up>42</span></div></div>',
  });

  contract({
    name: 'flat delta still renders',
    svelte: SvelteStatTile,
    astro: AstroStatTile,
    props: { label: 'Fires', value: '9', delta: 'today', flat: true },
    html:
      '<div class="bb-stat"><div class="bb-stat__head"><span class="bb-stat__label">Fires</span></div>' +
      '<div class="bb-stat__value"><span data-count-up>9</span></div>' +
      '<div class="bb-stat__delta" data-flat>today</div></div>',
  });
});

describe('Card flush and tone', () => {
  contract({
    name: 'flush accent card',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { flush: true, tone: 'accent' },
    slots: { default: 'body' },
    html: '<div class="bb-card bb-card--flush bb-card--accent" data-card>body</div>',
  });

  contract({
    name: 'danger section',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { as: 'section', tone: 'danger', id: 'danger-zone' },
    slots: { default: 'body' },
    html: '<section class="bb-card bb-card--danger" data-card id="danger-zone">body</section>',
  });
});

describe('Pager', () => {
  contract({
    name: 'first page disables previous',
    svelte: SveltePager,
    astro: AstroPager,
    props: { label: 'Page 1 of 3', prevHref: '?page=0', nextHref: '?page=2', hasPrev: false },
    html:
      '<div class="bb-pager">' +
      '<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Previous</span></a>' +
      '<span class="bb-pager__label">Page 1 of 3</span>' +
      '<a class="bb-btn bb-btn--ghost" href="?page=2" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Next</span></a></div>',
  });

  contract({
    name: 'host labels replace the defaults',
    svelte: SveltePager,
    astro: AstroPager,
    props: {
      label: 'Page 3 sur 3',
      prevHref: '?page=2',
      nextHref: '?page=4',
      hasNext: false,
      prevLabel: 'Précédent',
      nextLabel: 'Suivant',
    },
    html:
      '<div class="bb-pager">' +
      '<a class="bb-btn bb-btn--ghost" href="?page=2" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Précédent</span></a>' +
      '<span class="bb-pager__label">Page 3 sur 3</span>' +
      '<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark>' +
      '<i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Suivant</span></a></div>',
  });
});

describe('Disclosure', () => {
  contract({
    name: 'indexed and open',
    svelte: SvelteDisclosure,
    astro: AstroDisclosure,
    props: { summary: 'Can I cancel?', index: '01', open: true },
    slots: { default: '<p>Yes.</p>' },
    html:
      '<details class="bb-disclosure bb-disclosure--indexed" open>' +
      '<summary class="bb-disclosure__summary"><span class="bb-disclosure__index" aria-hidden="true">01</span>' +
      '<span class="bb-disclosure__label">Can I cancel?</span>' +
      '<span class="bb-disclosure__icon" aria-hidden="true"></span></summary>' +
      '<div class="bb-disclosure__body"><div class="bb-disclosure__content"><p>Yes.</p></div></div></details>',
  });

  contract({
    name: 'compact and closed',
    svelte: SvelteDisclosure,
    astro: AstroDisclosure,
    props: { summary: '12 commits', size: 'sm' },
    slots: { default: '<ul></ul>' },
    html:
      '<details class="bb-disclosure bb-disclosure--sm">' +
      '<summary class="bb-disclosure__summary"><span class="bb-disclosure__label">12 commits</span>' +
      '<span class="bb-disclosure__icon" aria-hidden="true"></span></summary>' +
      '<div class="bb-disclosure__body"><div class="bb-disclosure__content"><ul></ul></div></div></details>',
  });
});

const bar = (cls: string, aria: string, inner: string) =>
  `<div class="bb-progress ${cls}" role="progressbar" aria-label="Load" aria-valuemin="0" aria-valuemax="100" ${aria}>${inner}</div>`;

const FILL = '<span class="bb-progress__fill"></span>';

describe('ProgressBar', () => {
  contract({
    name: 'determinate',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: 0.4666, tone: 'success', label: 'Load' },
    html: bar('bb-progress--success', 'aria-valuenow="47" style="--progress: 0.4666;"', FILL),
  });

  contract({
    name: 'indeterminate',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: null, size: 'sm', label: 'Load' },
    html: bar('bb-progress--neutral bb-progress--sm bb-progress--indeterminate', 'aria-busy="true"', FILL),
  });

  contract({
    name: 'target marker is clamped and decorative',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: 0.5, tone: 'warning', target: 1.4, label: 'Load' },
    html: bar(
      'bb-progress--warning bb-progress--target',
      'aria-valuenow="50" style="--progress: 0.5;"',
      FILL + '<span class="bb-progress__target" aria-hidden="true" style="--target: 1;"></span>',
    ),
  });

  contract({
    name: 'segments replace the fill, one current',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: 0.5, size: 'sm', segments: ['warning', 'success', null], current: 1, label: 'Load' },
    html: bar(
      'bb-progress--neutral bb-progress--sm bb-progress--segmented',
      'aria-valuenow="50" style="--progress: 0.5;"',
      '<span class="bb-progress__seg bb-progress__seg--warning"></span>' +
        '<span class="bb-progress__seg bb-progress__seg--success is-current"></span>' +
        '<span class="bb-progress__seg"></span>',
    ),
  });
});

const CONTRACTS = [
  'card.css',
  'deck-layout.css',
  'deck-list.css',
  'disclosure.css',
  'fact-list.css',
  'feed.css',
  'management-row.css',
  'pager.css',
  'progress-bar.css',
];

for (const file of CONTRACTS) {
  test(`${file} reads brand tokens without literal fallbacks`, async () => {
    const css = await Bun.file(new URL(`../styles/elements/${file}`, import.meta.url)).text();
    expect(css).not.toMatch(/var\(--bb-[\w-]+\s*,/);
    expect(css).not.toMatch(/rgba\(\s*201\s*,\s*168\s*,\s*124/);
  });
}
