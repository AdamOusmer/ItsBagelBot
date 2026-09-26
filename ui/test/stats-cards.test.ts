// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import CounterCard from '../svelte/CounterCard.svelte';
import AstroCounterCard from '../astro/CounterCard.astro';
import CommunityCard from '../svelte/CommunityCard.svelte';
import AstroCommunityCard from '../astro/CommunityCard.astro';
import RankingCard from '../svelte/RankingCard.svelte';
import AstroRankingCard from '../astro/RankingCard.astro';

// Components are opaque to tsc; the framework compilers in loaders.ts and
// svelte-check validate their props. Both renderers run for every contract.
async function pair(
  svelte: any, astro: any, props: Record<string, unknown>,
  snippets: Record<string, unknown> = {}, slots: Record<string, string> = {},
) {
  const container = await experimental_AstroContainer.create();
  const svelteHtml = normalise(render(svelte, { props: { ...props, ...snippets } }).body);
  const astroHtml = normalise(await container.renderToString(astro, { props, slots }));
  expect(astroHtml).toBe(svelteHtml);
  return svelteHtml;
}

const artwork = '<i>Artwork</i>';
const raw = (html: string) => createRawSnippet(() => ({ render: () => html }));

test('CounterCard: default contract has no rate footer or artwork', async () => {
  const html = await pair(CounterCard, AstroCounterCard, { label: 'Requests', value: '128.4' });
  expect(html).toBe(
    '<article class="bb-counter-card bb-counter-card--green bb-counter-card--soft" aria-label="Requests">' +
    '<div class="bb-counter-card__body"><div class="bb-counter-card__head"><p class="bb-counter-card__label">Requests</p><p class="bb-counter-card__period">All time</p></div>' +
    '<div class="bb-counter-card__measure"><strong class="bb-counter-card__value">128.4</strong></div></div></article>',
  );
});

test('CounterCard: solid tan, tilt, caller artwork and zero rate agree', async () => {
  const html = await pair(CounterCard, AstroCounterCard, {
    label: 'Requests', value: '128.4', unit: 'M', detail: '128,400,000 processed',
    rate: '0', tone: 'tan', appearance: 'solid', tilt: 'right', class: 'featured', 'data-test': 'counter',
  }, { artwork: raw(artwork) }, { artwork });
  expect(html).toContain('bb-counter-card--tan bb-counter-card--solid bb-counter-card--tilt-right featured');
  expect(html).toContain('<span class="bb-counter-card__rate">0<small>/s</small></span>');
  expect(html).toContain('<div class="bb-counter-card__artwork" aria-hidden="true"><i>Artwork</i></div>');
});

test('CommunityCard: caller-owned cover artwork and lower content agree', async () => {
  const content = '<ol><li>Contributor</li></ol>';
  const html = await pair(CommunityCard, AstroCommunityCard, {
    title: 'Shared total', subtitle: 'Across all contributors', total: '82,416',
    appearance: 'solid', tone: 'green', period: 'This year',
  }, { artwork: raw(artwork), children: raw(content) }, { artwork, default: content });
  expect(html).toContain('bb-community-card--green bb-community-card--solid');
  expect(html).toContain('<div class="bb-community-card__body"><ol><li>Contributor</li></ol></div>');
  expect(html).toContain('<p class="bb-community-card__period">This year</p>');
});

test('CommunityCard: empty optional slots do not grow empty housings', async () => {
  const html = await pair(CommunityCard, AstroCommunityCard, { title: 'Shared total', total: '0', period: '' });
  expect(html).not.toContain('bb-community-card__body');
  expect(html).not.toContain('bb-community-card__artwork');
  expect(html).not.toContain('bb-community-card__period');
});

test('RankingCard: preserves caller order, compares real values and supports named artwork/actions', async () => {
  const items = [
    { id: 'small', label: 'Smaller', value: 25, valueLabel: '25', secondary: 'Other total: 8' },
    { id: 'large', label: 'Larger', href: '/large', value: 100, valueLabel: '100' },
  ];
  const actions = '<button type="button">Metric</button>';
  const html = await pair(RankingCard, AstroRankingCard, { title: 'Contributors', description: 'All totals', items }, {
    actions: raw(actions), leading: createRawSnippet<[{ id: string }, number]>((item, index) => ({
      render: () => `<i>${item().id}:${index()}</i>`,
    })),
  }, { actions, 'leading-small': '<i>small:0</i>', 'leading-large': '<i>large:1</i>' });
  expect(html.indexOf('Smaller')).toBeLessThan(html.indexOf('Larger'));
  expect(html).toContain('style="width:25%"');
  expect(html).toContain('style="width:100%"');
  expect(html).toContain('<span class="bb-ranking-card__artwork" aria-hidden="true"><i>small:0</i></span>');
  expect(html).toContain('<a class="bb-ranking-card__label" href="/large">Larger</a>');
});

test('RankingCard: zero, negative and nonfinite values do not produce invalid bar widths', async () => {
  const items = [0, -2, NaN, Infinity].map((value, index) => ({ id: `${index}`, label: `Item ${index}`, value, valueLabel: '—' }));
  const html = await pair(RankingCard, AstroRankingCard, { title: 'Totals', items });
  expect(html.match(/style="width:0%"/g)).toHaveLength(4);
  expect(html).not.toContain('NaN');
  expect(html).not.toContain('Infinity');
});

test('RankingCard: fractional values compare to their largest value', async () => {
  const items = [0.25, 0.5].map((value, index) => ({ id: `${index}`, label: `Item ${index}`, value, valueLabel: `${value}` }));
  const html = await pair(RankingCard, AstroRankingCard, { title: 'Shares', items });
  expect(html).toContain('style="width:50%"');
  expect(html).toContain('style="width:100%"');
});

test('RankingCard: empty state is caller supplied', async () => {
  const html = await pair(RankingCard, AstroRankingCard, { title: 'Totals', items: [], emptyLabel: 'Waiting for data' });
  expect(html).toContain('<p class="bb-ranking-card__empty">Waiting for data</p>');
  expect(html).not.toContain('<ol');
});


test('RankingCard: exact textual totals above the safe integer limit stay intact', async () => {
  const items = [
    { id: 'first', label: 'First', value: 50, valueLabel: '9,007,199,254,740,995', secondary: 'Other total: 9,007,199,254,740,993' },
    { id: 'second', label: 'Second', value: 100, valueLabel: '18,014,398,509,481,990' },
  ];
  const html = await pair(RankingCard, AstroRankingCard, { title: 'Exact totals', items });
  expect(html).toContain('9,007,199,254,740,995');
  expect(html).toContain('18,014,398,509,481,990');
  expect(html).toContain('Other total: 9,007,199,254,740,993');
  expect(html).toContain('style="width:50%"');
});
