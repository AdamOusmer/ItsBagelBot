// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { stubSvelteKit } from '../../../../test/sveltekit';

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
mock.module('#lib/autosave.svelte', () => ({
  Autosave: class {
    status = {};
    revision = 0;
    schedule() {}
    flush() {}
    reset() {}
  }
}));

const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Input.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: OverlaysPage } = await import('./+page.svelte');
const { default: CreatePortal } = await import('./CreatePortal.svelte');

const system = { id: 7, kind: 'system', template: 'powered', name: 'Powered by Bagel', revision: 3 };
const custom = { id: 8, kind: 'custom', name: 'Starting scene', revision: 1 };

function renderPage(data: Record<string, unknown>) {
  return render(OverlaysPage, { props: { data: { overlays: [system], ...data } } as never }).body;
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
  { name: 'a delegate', data: { readOnly: true, overlays: null }, text: 'Only the channel owner can change overlays.' },
  { name: 'an unavailable owner', data: { readOnly: false, overlays: null }, text: 'Overlay settings are unavailable.' }
])('shows a notice and no banner for $name', ({ data, text }) => {
  const html = renderPage({ ...data, isPremium: false });
  expect([html.includes(text), html.includes('A small badge slides onto your stream')]).toEqual([true, false]);
});

const bannerCases: { name: string; overlays: object[]; inert: boolean[] }[] = [
  { name: 'added', overlays: [system], inert: [true, false] },
  { name: 'not added', overlays: [], inert: [false, true] }
];

test.each(bannerCases)('the Powered by Bagel banner keeps both action sets and disables the unused one when $name', async ({ overlays, inert }) => {
  const html = renderPage({ readOnly: false, overlays, isPremium: false });
  const slots = await attributes(html, '.powered-banner .slot');
  const links = await attributes(html, 'a[href="/overlays/powered"]');
  expect([slots.map((slot) => 'inert' in slot), links.length, html.includes('Add to my stream')]).toEqual([inert, 1, true]);
});

test('the hub lists custom overlays only, each with its own actions menu', async () => {
  const html = renderPage({ readOnly: false, overlays: [system, custom], isPremium: true });
  const menus = await attributes(html, 'button[aria-haspopup="dialog"]');
  expect([
    menus.map((menu) => menu['aria-label']),
    html.includes('Starting scene'),
    html.includes('<iframe'),
    html.includes('Add Powered by Bagel')
  ]).toEqual([['More actions for Starting scene'], true, false, false]);
});

test('an empty list shows a card that opens the portal', () => {
  const html = renderPage({ readOnly: false, overlays: [], isPremium: true });
  expect(html.includes('Create your first overlay')).toBe(true);
});

test.each([{ isPremium: true }, { isPremium: false }])('offers import and create as buttons that open the portal, premium=$isPremium', ({ isPremium }) => {
  const html = renderPage({ readOnly: false, overlays: [], isPremium });
  expect(['Import', 'New overlay'].map((label) => html.includes(label))).toEqual([true, true]);
  expect(html.includes('href="/overlays/custom"')).toBe(false);
});

test('only custom overlays get an edit link to their editor', async () => {
  const html = renderPage({ readOnly: false, overlays: [system, custom], isPremium: true });
  const links = await attributes(html, 'a[href^="/overlays/custom"]');
  expect(links.map((link) => [link.href, link['aria-label']])).toEqual([['/overlays/custom?id=8', 'Edit Starting scene']]);
});

test('the creation portal stays closed until asked', () => {
  const html = renderPage({ readOnly: false, overlays: [], isPremium: true });
  expect(['Blank canvas', 'Import a zip'].map((label) => html.includes(label))).toEqual([false, false]);
});

test('the open portal preselects the blank canvas and offers every canvas size', async () => {
  const html = render(CreatePortal, { props: { open: true, premium: true } as never }).body;
  const tiles = await attributes(html, 'button[aria-pressed]');
  const sizes = await attributes(html, 'select option');
  expect([
    tiles.map((tile) => tile['aria-pressed']),
    sizes.map((size) => size.value),
    ['Blank canvas', 'Import a zip', 'Canvas size', 'Start', 'Cancel'].map((label) => html.includes(label))
  ]).toEqual([
    ['true', 'false', 'false', 'false'],
    ['1920x1080', '1280x720', '2560x1440', '3840x2160', '1080x1920'],
    [true, true, true, true, true]
  ]);
});
