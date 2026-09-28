// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import StatsPageLayout from './StatsPageLayout.svelte';

const slots = {
  heading: '<h1>Stats.</h1><p>All the activity.</p>',
  crowd: '<span><svg></svg></span><span><svg></svg></span>',
  counters: '<article>Messages</article><article>Events</article>',
  ranking: '<section>Channels</section>',
  community: '<section>Feedings</section>',
  notice: '<p role="status">Waiting for fresh data.</p>',
  footer: '<p>Updated live.</p>',
};
const required = ['heading', 'counters', 'ranking', 'community'];

for (const arrangement of ['playful', 'onboarding', 'gathering'] as const) {
  for (const optional of [false, true]) {
    test(`StatsPageLayout renders ${arrangement}, optional slots ${optional}`, () => {
      const chosen = Object.entries(slots).filter(([name]) => optional || required.includes(name));
      const snippets = Object.fromEntries(
        chosen.map(([name, html]) => [name, createRawSnippet(() => ({ render: () => html }))]),
      );
      const html = render(StatsPageLayout, { props: { arrangement, class: 'preview', ...snippets } as never }).body
        .replace(/<!--[\s\S]*?-->/g, '');
      expect(html).toContain(`class="stats-page stats-page--${arrangement} preview"`);
      expect(html).toContain('bb-ambient-sky bb-ambient-sky--fixed');
      expect(html).toContain('<div class="stats-page__counters"><article>Messages</article><article>Events</article></div>');
      expect(html.includes('stats-page__notice')).toBe(optional);
      expect(html.includes('stats-page__crowd')).toBe(optional);
      expect(html.includes('stats-page__footer')).toBe(optional);
    });
  }
}
