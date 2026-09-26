// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import SvelteSky from '../svelte/AmbientSky.svelte';
import StageSky from '../svelte/Sky.svelte';
import AstroSky from '../astro/AmbientSky.astro';
import SvelteLayout from '../svelte/StatsPageLayout.svelte';
import AstroLayout from '../astro/StatsPageLayout.astro';

for (const props of [
  { uid: 'sky-fixture' },
  { uid: 'sky-fixture', position: 'contained', shift: -0.4, turn: 24, px: 0.2, py: -0.3, progress: 1.5, leaving: true, warmth: 0.4, class: 'preview', 'data-preview': 'sky' },
  { uid: 'sky-fixture', progress: -1 },
]) {
  test(`AmbientSky adapters agree: ${JSON.stringify(props)}`, async () => {
    const container = await experimental_AstroContainer.create();
    const svelte = normalise(render(SvelteSky, { props: props as never }).body);
    const astro = normalise(await container.renderToString(AstroSky, { props }));
    expect(svelte).toBe(astro);
    expect(svelte).toContain('aria-hidden="true"');
    expect(svelte).toContain('class="bb-light-field" data-field');
    expect(svelte).toContain('id="sky-fixture-green"');
    expect(svelte).toContain('stroke="url(#sky-fixture-green)"');
    expect(svelte).toContain(`--ambient-progress: ${Math.min(Math.max(props.progress ?? 0, 0), 1)};`);
  });
}

// Generated per-instance SVG IDs belong to their framework, while both
// adapters must retain the same gradient references and all contract markup.
function normaliseSkyId(html: string): string {
  const base = html.match(/<linearGradient id="([^"]+)-green"/)?.[1];
  return normalise(base ? html.replaceAll(base, 'sky-instance') : html);
}
const slots = {
  heading: '<h1>Stats.</h1><p>All the activity.</p>',
  crowd: '<span><svg></svg></span><span><svg></svg></span>',
  counters: '<article>Messages</article><article>Events</article>',
  ranking: '<section>Channels</section>',
  community: '<section>Feedings</section>',
  notice: '<p role="status">Waiting for fresh data.</p>',
  footer: '<p>Updated live.</p>',
};
for (const arrangement of ['playful', 'onboarding', 'gathering'] as const) {
  for (const optional of [false, true]) {
    test(`StatsPageLayout adapters agree: ${arrangement}, optional slots ${optional}`, async () => {
      const chosen = Object.fromEntries(Object.entries(slots).filter(([name]) => optional || ['heading', 'counters', 'ranking', 'community'].includes(name)));
      const snippets = Object.fromEntries(Object.entries(chosen).map(([name, html]) => [name, createRawSnippet(() => ({ render: () => html }))]));
      const container = await experimental_AstroContainer.create();
      const svelte = normaliseSkyId(render(SvelteLayout, { props: { arrangement, class: 'preview', ...snippets } as never }).body);
      const astro = normaliseSkyId(await container.renderToString(AstroLayout, { props: { arrangement, class: 'preview' }, slots: chosen }));
      expect(svelte).toBe(astro);
      expect(svelte).toContain(`class="bb-stats-page bb-stats-page--${arrangement} preview"`);
      expect(svelte).toContain('class="bb-ambient-sky bb-ambient-sky--fixed"');
      expect(svelte).toContain('<div class="bb-stats-page__counters"><article>Messages</article><article>Events</article></div>');
      expect(svelte.includes('bb-stats-page__notice')).toBe(optional);
      expect(svelte.includes('bb-stats-page__crowd')).toBe(optional);
      expect(svelte.includes('bb-stats-page__footer')).toBe(optional);
    });
  }
}

// Existing welcome, import and deploy stages retain their fixed sky and all
// pointer/journey props; no second implementation should grow in the wrapper.
for (const props of [{}, { shift: -1, turn: 48, px: 0.25, py: -0.2, progress: 0.8, leaving: true }]) {
  test(`Sky stage wrapper preserves the shared atmosphere: ${JSON.stringify(props)}`, () => {
    const wrapper = normaliseSkyId(render(StageSky, { props }).body);
    const direct = normaliseSkyId(render(SvelteSky, { props }).body);
    expect(wrapper).toBe(direct);
    expect(wrapper).toContain('bb-ambient-sky--fixed');
    expect(wrapper).toContain('data-warmth="0.7"');
  });
}
