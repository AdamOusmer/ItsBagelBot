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

const powered = {
  url: 'https://overlay.itsbagelbot.com/powered#AAAA',
  config: { ...defaultPoweredConfig(), show_seconds: 12, directions: ['left'], align: 'center' },
  revision: 3
};

function renderPage(data: Record<string, unknown>) {
  return render(OverlaysPage, { props: { data, form: null } as never }).body;
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

test('the only selected edge cannot be switched off', async () => {
  const html = renderPage({ readOnly: false, powered, isPremium: false });
  const chips = await attributes(html, 'button.bb-chip');
  expect(chips.map((chip) => [chip['aria-pressed'], 'disabled' in chip])).toEqual([
    ['false', false], ['false', false], ['true', true], ['false', false]
  ]);
});

test.each([
  { name: 'a delegate', data: { readOnly: true, powered: null }, text: 'Only the channel owner can change overlays.' },
  { name: 'an unavailable owner', data: { readOnly: false, powered: null }, text: 'Overlay settings are unavailable.' }
])('shows a notice and no controls for $name', ({ data, text }) => {
  const html = renderPage({ ...data, isPremium: false });
  expect([html.includes(text), html.includes('<iframe')]).toEqual([true, false]);
});
