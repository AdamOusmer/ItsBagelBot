// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();
const pageState = { data: {}, url: new URL('http://localhost/overlays/custom') };
mock.module('$app/state', () => ({ navigating: {}, page: pageState, updated: {} }));
mock.module('@bagel/ui/svelte/discard-guard', () => ({
  createDiscardGuard: () => ({ open: false, guard: (action: () => void) => action(), confirm: () => {}, cancel: () => {} })
}));
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
const base = { readOnly: false, overlay: null, missing: false };

function renderPage(data: Record<string, unknown>, search = '') {
  pageState.url = new URL(`http://localhost/overlays/custom${search}`);
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

test('an owner sees the editor canvas', () => {
  expect(renderPage({}).includes('bb-overlay-canvas')).toBe(true);
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
  const html = renderPage({ overlay: served });
  expect(html.includes('Alert layer')).toBe(true);
});

test('a delegate gets the owner-only notice and no editor', () => {
  const html = renderPage({ readOnly: true });
  expect([html.includes('Only the channel owner can change overlays.'), html.includes('bb-overlay-canvas')]).toEqual([true, false]);
});

test.each([
  { name: 'a blank editor by default', search: '', title: 'New overlay', layers: [] },
  { name: 'a blank editor for ?template=blank', search: '?template=blank', title: 'New overlay', layers: [] },
  { name: 'a blank editor for an unknown template', search: '?template=nope', title: 'New overlay', layers: [] },
  { name: 'the alerts template', search: '?template=alerts', title: 'Alerts', layers: ['Alert box'] },
  { name: 'the just chatting template', search: '?template=justChatting', title: 'Just chatting', layers: ['Chat box', 'Alert box', 'Goal', 'Event list'] },
  { name: 'the starting soon template', search: '?template=startingSoon', title: 'Starting soon', layers: ['Timer', 'Label'] }
])('opens $name', ({ search, title, layers }) => {
  const html = renderPage({}, search);
  const present = ['Alert box', 'Chat box', 'Goal', 'Event list', 'Timer', 'Label'].filter((name) => html.includes(`>${name}</span>`));
  expect({ title: html.includes(`value="${title}"`), layers: present.sort() }).toEqual({ title: true, layers: [...layers].sort() });
});

test('a saved overlay ignores the template in the URL', () => {
  const html = renderPage({ overlay: served }, '?template=startingSoon');
  expect([html.includes('>Alert layer</span>'), html.includes('>Timer</span>')]).toEqual([true, false]);
});

test.each([
  { name: 'a blank editor at 1280 x 720', search: '?size=1280x720', frame: '1280 × 720' },
  { name: 'the alerts template at 1080 x 1920', search: '?template=alerts&size=1080x1920', frame: '1080 × 1920' },
  { name: 'the default size for a size that is not offered', search: '?size=999x1', frame: '1920 × 1080' },
  { name: 'the default size for a malformed size', search: '?size=big', frame: '1920 × 1080' },
  { name: 'the default size with no size', search: '', frame: '1920 × 1080' }
])('seeds $name', ({ search, frame }) => {
  expect(renderPage({}, search).includes(`>${frame}</span>`)).toBe(true);
});

test('a saved overlay ignores the size in the URL', () => {
  const html = renderPage({ overlay: served }, '?size=1280x720');
  expect([html.includes('>1920 × 1080</span>'), html.includes('>1280 × 720</span>')]).toEqual([true, false]);
});

test.each([
  { text: 'Start by adding a widget', present: true },
  { text: 'Add widget', present: true },
  { text: 'Layers', present: true },
  { text: 'Emulate', present: true },
  { text: 'Overlay settings', present: true },
  { text: 'Dot grid', present: true },
  { text: 'Editor tools', present: false },
  { text: 'Layer settings', present: false }
])('the editor shows "$text": $present', ({ text, present }) => {
  expect(renderPage({}).includes(text)).toBe(present);
});

test('the editor stands alone, with no import panel, page head or template buttons', () => {
  const html = renderPage({});
  expect(['Import an overlay', 'Start from', 'Just chatting'].map((text) => html.includes(text))).toEqual([false, false, false]);
});
