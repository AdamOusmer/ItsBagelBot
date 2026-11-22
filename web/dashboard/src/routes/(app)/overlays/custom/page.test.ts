// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
const { translate } = await import('@bagel/kit/i18n');
const english = {
  t: (key: string, params?: Record<string, string | number>) => translate('en', key, params),
  tl: () => [],
  locale: 'en'
};
mock.module('@bagel/kit/i18n/context', () => ({
  getI18n: () => english,
  setI18n: () => english
}));

const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Input.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: CustomPage } = await import('./+page.svelte');
const { default: MediaControl } = await import('@bagel/kit/components/overlay-editor/MediaControl.svelte');
const { createOverlay } = await import('@bagel/kit/overlay-editor');

const served = { id: 8, name: 'Alerts', revision: 3, document: createOverlay('alerts', 'Alerts', () => 'Alert layer') };
const base = { readOnly: false, preview: false, overlay: null, missing: false };

function renderPage(data: Record<string, unknown>) {
  return render(CustomPage, { props: { data: { ...base, ...data } } as never }).body;
}

async function attributes(html: string, selector: string) {
  const found: Record<string, string>[] = [];
  await new HTMLRewriter().on(selector, {
    element(el) {
      found.push(Object.fromEntries(el.attributes));
    }
  }).transform(new Response(html)).text();
  return found;
}

test.each([
  { name: 'a premium owner', data: { isPremium: true }, canvas: true, gate: false, upsell: false },
  { name: 'a free owner', data: { isPremium: false }, canvas: false, gate: true, upsell: false },
  { name: 'a free owner trying the editor', data: { isPremium: false, preview: true }, canvas: true, gate: false, upsell: true }
])('shows the right surface to $name', ({ data, canvas, gate, upsell }) => {
  const html = renderPage(data);
  expect({
    canvas: html.includes('bb-overlay-canvas'),
    gate: html.includes('premium-gate-title'),
    upsell: html.includes('Saving, uploads and publishing need Premium.')
  }).toEqual({ canvas, gate, upsell });
});

test('the gate offers the editor preview', async () => {
  const links = await attributes(renderPage({ isPremium: false }), 'a[href="/overlays/custom?preview=1"]');
  expect(links).toHaveLength(1);
});

test.each([
  { name: 'an upload adapter', binding: { upload: async () => 'asset:one', assets: {} }, disabled: false },
  { name: 'the preview', binding: undefined, disabled: true }
])('a media field with $name has its file input disabled=$disabled', async ({ binding, disabled }) => {
  const field = { key: 'media', type: 'media', slot: 'image' };
  const html = render(MediaControl, { props: { field, value: '', update: () => {}, binding } as never }).body;
  const inputs = await attributes(html, 'input[type="file"]');
  expect(inputs.map((input) => 'disabled' in input)).toEqual([disabled]);
});

test('a saved overlay opens with its layers', () => {
  const html = renderPage({ isPremium: true, overlay: served });
  expect(html.includes('Alert layer')).toBe(true);
});

test('a delegate gets the owner-only notice and no editor', () => {
  const html = renderPage({ isPremium: true, readOnly: true });
  expect([html.includes('Only the channel owner can change overlays.'), html.includes('bb-overlay-canvas')]).toEqual([true, false]);
});

test('the import entry is offered next to the editor', () => {
  expect(renderPage({ isPremium: true }).includes('Import an overlay')).toBe(true);
});
