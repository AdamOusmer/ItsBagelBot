// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import Switch from '../svelte/Switch.svelte';
import AstroSwitch from '../astro/Switch.astro';
import EmptyState from '../svelte/EmptyState.svelte';
import AstroEmptyState from '../astro/EmptyState.astro';
import Field from '../svelte/Field.svelte';
import AstroField from '../astro/Field.astro';
import StatTile from '../svelte/StatTile.svelte';
import AstroStatTile from '../astro/StatTile.astro';
import Card from '../svelte/Card.svelte';
import AstroCard from '../astro/Card.astro';
import { createRawSnippet } from 'svelte';

const children = createRawSnippet(() => ({ render: () => '<span>x</span>' }));

async function both(svelte: unknown, astro: unknown, props: Record<string, unknown>) {
  const container = await experimental_AstroContainer.create();
  return {
    svelte: normalise(render(svelte as never, { props: { ...props, children } as never }).body),
    astro: normalise(await container.renderToString(astro as never, { props, slots: { default: '<span>x</span>' } })),
  };
}

const CASES: [string, unknown, unknown, Record<string, unknown>, string][] = [
  ['Switch', Switch, AstroSwitch, { label: 'Enable' }, 'bb-switch'],
  ['EmptyState', EmptyState, AstroEmptyState, { title: 'Nothing yet' }, 'bb-empty'],
  ['Field', Field, AstroField, { label: 'Name' }, 'bb-field'],
  ['StatTile', StatTile, AstroStatTile, { label: 'Views', value: '3', delta: '+1' }, 'bb-stat'],
];

for (const [name, svelte, astro, props, contract] of CASES) {
  test(`${name}: a consumer class joins the contract class in both adapters`, async () => {
    const html = await both(svelte, astro, { ...props, class: 'host', 'data-probe': 'yes' });
    for (const out of [html.svelte, html.astro]) {
      expect(out).toContain(`class="${contract} host"`);
      expect(out).toContain('data-probe="yes"');
    }
  });
}

test('Card: href without as renders a link in both adapters', async () => {
  const html = await both(Card, AstroCard, { href: '/stats' });
  for (const out of [html.svelte, html.astro]) expect(out).toMatch(/^<a class="bb-card[^"]*" href="\/stats"/);
});
