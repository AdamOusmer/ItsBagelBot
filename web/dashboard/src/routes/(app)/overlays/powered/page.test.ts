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
import { defaultPoweredConfig } from '@bagel/kit/overlays';

const { default: PoweredPage } = await import('./+page.svelte');

const powered = {
  id: 7,
  config: { ...defaultPoweredConfig(), show_seconds: 12, directions: ['left'], align: 'center' },
  revision: 3
};

function renderPage(data: Record<string, unknown>) {
  return render(PoweredPage, { props: { data: { powered, ...data } } as never }).body;
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
  const html = renderPage({ isPremium: premium });
  const [frame] = await attributes(html, 'iframe');
  const src = new URL((frame?.src ?? '').replaceAll('&amp;', '&'));
  expect([src.origin + src.pathname, Object.fromEntries(src.searchParams)]).toEqual([
    'https://overlay.itsbagelbot.com/powered', { preview: '1', show: '12', from: 'left', align: 'center', premium: flag }
  ]);
});

test('the entry edge is a single choice with the saved edge checked', async () => {
  const html = renderPage({ isPremium: false });
  const [group] = await attributes(html, '[role="radiogroup"][aria-label="Comes in from"]');
  const edges = await attributes(html, '[role="radiogroup"][aria-label="Comes in from"] button[role="radio"]');
  expect([Boolean(group), edges.map((edge) => edge['aria-checked'])]).toEqual([true, ['false', 'false', 'true', 'false']]);
});

test('offers a way back to the hub and the replace link action', async () => {
  const html = renderPage({ isPremium: false });
  const back = await attributes(html, 'a[href="/overlays/hub"]');
  const forms = await attributes(html, 'form[action="?/rotate"]');
  expect([back.length, forms.length]).toEqual([1, 1]);
});

test('shows a notice and no controls when the settings could not be read', () => {
  const html = renderPage({ powered: null, isPremium: false });
  expect([html.includes('Overlay settings are unavailable.'), html.includes('<iframe')]).toEqual([true, false]);
});
