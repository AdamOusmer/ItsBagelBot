// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { createRawSnippet } from 'svelte';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import StatusDot from '../svelte/StatusDot.svelte';
import AstroStatusDot from '../astro/StatusDot.astro';
import Badge from '../svelte/Badge.svelte';
import AstroBadge from '../astro/Badge.astro';
import Input from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';

for (const tone of ['success', 'warning', 'error', 'neutral'] as const) {
  test(`status dot ${tone} is decorative in both adapters`, async () => {
    const expected = `<span class="bb-status-dot ${tone}" aria-hidden="true"></span>`;
    expect(normalise(render(StatusDot, { props: { tone } }).body)).toBe(expected);
    const astro = await experimental_AstroContainer.create();
    expect(normalise(await astro.renderToString(AstroStatusDot, { props: { tone } }))).toBe(expected);
  });
}

test('pill badges preserve non-interactive label semantics in both adapters', async () => {
  const props = { shape: 'pill' as const, class: 'tier' };
  const expected = '<span class="bb-tag bb-badge bb-badge--pill tier">Paid</span>';
  const children = createRawSnippet(() => ({ render: () => 'Paid' }));
  expect(normalise(render(Badge, { props: { ...props, children } }).body)).toBe(expected);
  const astro = await experimental_AstroContainer.create();
  expect(normalise(await astro.renderToString(AstroBadge, { props, slots: { default: 'Paid' } }))).toBe(expected);
});

for (const props of [{ type: 'number' as const, value: 12 }, { type: 'datetime-local' as const, value: '2026-09-14T10:30' }, { type: 'date' as const, value: '2026-09-14' }]) {
  test(`Input ${props.type} preserves native value and form attributes across adapters`, async () => {
    const field = { ...props, name: 'value', required: true };
    const svelte = normalise(render(Input, { props: field }).body);
    const astro = await experimental_AstroContainer.create();
    expect(normalise(await astro.renderToString(AstroInput, { props: field }))).toBe(svelte);
    expect(svelte).toContain(`type="${props.type}"`);
    expect(svelte).toContain(`value="${props.value}"`);
    expect(svelte).toContain('name="value"');
  });
}
