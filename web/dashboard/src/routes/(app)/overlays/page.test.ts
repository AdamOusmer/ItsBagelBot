// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { defaultPoweredConfig } from '@bagel/kit/overlays';
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

const system = { id: 7, kind: 'system', template: 'powered', name: 'Powered by Bagel', revision: 3 };
const custom = { id: 8, kind: 'custom', name: 'Starting scene', revision: 1 };
const powered = {
  id: 7,
  config: { ...defaultPoweredConfig(), show_seconds: 12, directions: ['left'], align: 'center' },
  revision: 3
};

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
  { premium: true, flag: '1' },
  { premium: false, flag: '0' }
])('previews the saved look with premium=$flag and no token', async ({ premium, flag }) => {
  const html = renderPage({ readOnly: false, powered, isPremium: premium });
  const [frame] = await attributes(html, 'iframe');
  const src = new URL((frame?.src ?? '').replaceAll('&amp;', '&'));
  expect([src.origin + src.pathname, Object.fromEntries(src.searchParams)]).toEqual([
    'https://overlay.itsbagelbot.com/powered', { preview: '1', show: '12', from: 'left', align: 'center', premium: flag }
  ]);
});

test('the entry edge is a single choice with the saved edge checked', async () => {
  const html = renderPage({ readOnly: false, powered, isPremium: false });
  const [group] = await attributes(html, '[role="radiogroup"][aria-label="Comes in from"]');
  const edges = await attributes(html, '[role="radiogroup"][aria-label="Comes in from"] button[role="radio"]');
  expect([Boolean(group), edges.map((edge) => edge['aria-checked'])]).toEqual([true, ['false', 'false', 'true', 'false']]);
});

test.each([
  { name: 'a delegate', data: { readOnly: true, overlays: null, powered: null }, text: 'Only the channel owner can change overlays.' },
  { name: 'an unavailable owner', data: { readOnly: false, overlays: null, powered: null }, text: 'Overlay settings are unavailable.' }
])('shows a notice and no controls for $name', ({ data, text }) => {
  const html = renderPage({ ...data, isPremium: false });
  expect([html.includes(text), html.includes('<iframe')]).toEqual([true, false]);
});

test.each([
  { name: 'added', overlays: [system], disabled: true },
  { name: 'not added', overlays: [], disabled: false }
])('Add Powered by Bagel stays in the page and is disabled when $name', async ({ overlays, disabled }) => {
  const html = renderPage({ readOnly: false, overlays, powered: null, isPremium: false });
  const buttons = await attributes(html, 'form[action="?/add"] button');
  expect(buttons.map((button) => 'disabled' in button)).toEqual([disabled]);
});

test('lists each overlay with a replace action and deletes only custom ones', async () => {
  const html = renderPage({ readOnly: false, overlays: [system, custom], powered, isPremium: true });
  const replaced = await attributes(html, 'form[action="?/rotate"] input[name="id"]');
  expect([replaced.map((input) => input.value), html.includes('Powered by Bagel'), html.includes('Starting scene')]).toEqual([['7', '8'], true, true]);
  expect(html.match(/>Delete</g)).toHaveLength(1);
});

test('the custom overlay entry is always offered', async () => {
  const [link] = await attributes(renderPage({ readOnly: false, overlays: [], powered: null, isPremium: false }), 'a[href="/overlays/custom"]');
  expect(Boolean(link)).toBe(true);
});
