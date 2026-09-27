// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { experimental_AstroContainer } from 'astro/container';
import AstroSelect from '../astro/Select.astro';
import SvelteSelect from '../svelte/Select.svelte';
import { render } from 'svelte/server';
import { normalise } from './normalise';

// Select adapters deliberately differ during SSR: Astro keeps an immediately
// usable native fallback, then enhances it into the same interactive picker.
async function astroSelect(props: Record<string, unknown>, slot?: string) {
  const container = await experimental_AstroContainer.create();
  return normalise(await container.renderToString(AstroSelect, {
    props,
    ...(slot === undefined ? {} : { slots: { default: slot } }),
  }));
}

test('Astro Select preserves option slots and native form attributes before enhancement', async () => {
  const html = await astroSelect({ id: 'mode', name: 'mode', required: true, disabled: true, invalid: true, fill: true },
    '<optgroup label="Modes" disabled><option value="a" selected>A</option><option value="b" disabled>B</option></optgroup>');
  expect(html).toContain('class="bb-input bb-input--select bb-input--fill" data-select-fallback data-invalid');
  expect(html).toContain('<select id="mode" name="mode" required disabled>');
  expect(html).toContain('<optgroup label="Modes" disabled><option value="a" selected>A</option><option value="b" disabled>B</option></optgroup>');
  expect(html).not.toContain('data-searchable');
});

test('Astro Select serializes optional search labels and option metadata for the custom picker', async () => {
  const html = await astroSelect({
    name: 'zone', value: 'America/Toronto', searchable: true, label: 'Timezone',
    searchPlaceholder: 'Find a city', searchClearLabel: 'Clear city', emptyLabel: 'No cities',
    options: [
      { value: 'America/Toronto', label: 'Toronto', description: 'America/Toronto', triggerLabel: 'America/Toronto', group: 'America', searchText: 'Canada' },
      { value: 'America/Montreal', label: 'Montréal', disabled: true },
    ],
  });
  expect(html).toContain('data-searchable');
  expect(html).toContain('data-label="Timezone"');
  expect(html).toContain('data-search-placeholder="Find a city"');
  expect(html).toContain('data-search-clear-label="Clear city"');
  expect(html).toContain('data-empty-label="No cities"');
  expect(html).toContain('<option value="America/Toronto" selected data-description="America/Toronto" data-group="America" data-search-text="Canada" data-trigger-label="America/Toronto">Toronto</option>');
  expect(html).toContain('<option value="America/Montreal" disabled>Montréal</option>');
});

test('Svelte Select keeps selected form value, disabled options, and fallback validation during SSR', () => {
  const html = normalise(render(SvelteSelect, { props: {
    id: 'zone', name: 'zone', form: 'timer', value: 'America/Toronto', required: true,
    options: [
      { value: 'UTC', label: 'UTC', disabled: true },
      { value: 'America/Toronto', label: 'Toronto', triggerLabel: 'America/Toronto' },
    ],
  } }).body);
  expect(html).toContain('<select id="zone" name="zone" form="timer" required>');
  expect(html).toContain('<option value="UTC" disabled>UTC</option>');
  expect(html).toContain('<option value="America/Toronto" selected>America/Toronto</option>');
  expect(html).toContain('bb-select__trigger');
  expect(html).toContain('hidden');
});

test('Svelte Select represents an existing value absent from its refreshed option list', () => {
  const html = normalise(render(SvelteSelect, { props: {
    id: 'stored', name: 'stored', value: 'saved-id', options: [], disabled: true,
  } }).body);
  expect(html).toContain('<select id="stored" name="stored" disabled>');
  expect(html).toContain('<option value="saved-id" selected>saved-id</option>');
  expect(html).toContain('<span class="bb-select__value">saved-id</span>');
});

test('Astro Select preserves a stored value omitted from refreshed options during SSR', async () => {
  const html = await astroSelect({ id: 'stored', name: 'stored', value: 'saved-id', options: [] });
  expect(html).toContain('<select id="stored" name="stored">');
  expect(html).toContain('<option value="saved-id" selected data-select-fallback-option>saved-id</option>');
});
