// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import AstroSelect from '../astro/Select.astro';
import SvelteSelect from '../svelte/Select.svelte';
import PickerPanel from '../svelte/PickerPanel.svelte';
import { astroHtml, svelteHtml } from './contract';

const astroSelect = (props: Record<string, unknown>, slot?: string) =>
  astroHtml(AstroSelect, props, slot === undefined ? {} : { default: slot });

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
  const html = svelteHtml(SvelteSelect, {
    id: 'zone', name: 'zone', form: 'timer', value: 'America/Toronto', required: true,
    options: [
      { value: 'UTC', label: 'UTC', disabled: true },
      { value: 'America/Toronto', label: 'Toronto', triggerLabel: 'America/Toronto' },
    ],
  });
  expect(html).toContain('<select id="zone" name="zone" form="timer" required>');
  expect(html).toContain('<option value="UTC" disabled>UTC</option>');
  expect(html).toContain('<option value="America/Toronto" selected>America/Toronto</option>');
  expect(html).toContain('bb-select__trigger');
  expect(html).toContain('hidden');
});

test('Svelte Select submits nothing and shows the placeholder for a value absent from its options', () => {
  const html = svelteHtml(SvelteSelect, {
    id: 'stored', name: 'stored', value: 'saved-id', options: [{ value: 'live', label: 'Live' }], disabled: true,
  });
  expect(html).toContain('<select id="stored" name="stored" disabled><option value selected disabled hidden>Select…</option><option value="live">Live</option></select>');
  expect(html).not.toContain('saved-id');
  expect(html).toContain('data-placeholder');
  expect(html).toContain('<span class="bb-select__value">Select…</span>');
});

test('Svelte Select omits the placeholder option when the value is offered', () => {
  const html = svelteHtml(SvelteSelect, {
    name: 'mode', value: '', options: [{ value: '', label: 'None' }, { value: 'a', label: 'A' }],
  });
  expect(html).not.toContain('disabled hidden');
  expect(html).toContain('<option value selected>None</option>');
});

test('Astro Select submits nothing and shows the placeholder for a value absent from its options', async () => {
  const html = await astroSelect({ id: 'stored', name: 'stored', value: 'saved-id', options: [{ value: 'live', label: 'Live' }] });
  expect(html).toContain('<select id="stored" name="stored"><option value selected disabled hidden data-select-fallback-option>Select…</option><option value="live">Live</option></select>');
  expect(html).not.toContain('>saved-id<');
});

test('an open Svelte picker keeps wheel and touch away from the page smooth-scroller', () => {
  const children = createRawSnippet(() => ({ render: () => '<ul role="listbox"></ul>' }));
  for (const placement of ['beside', 'below'] as const) {
    const html = render(PickerPanel, { props: { open: true, label: 'Pick', placement, onClose: () => {}, children } }).body;
    expect(html).toContain('class="bb-picker-panel bb-picker-panel--dropdown" data-overlay="" data-lenis-prevent=""');
  }
});
