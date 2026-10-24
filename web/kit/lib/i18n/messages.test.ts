// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, mock, test } from 'bun:test';

mock.module('virtual:bagel-i18n', () => ({ locales: ['en', 'fr'], localeNames: {}, urls: {}, loaders: {}, sliced: true }));

const { ensureCatalog, registerMessages, translate } = await import('./messages');

const CHUNKS = 'https://dash.example/_app/immutable/nodes/';
const realFetch = globalThis.fetch;
const requested: string[] = [];

function serve(slices: Record<string, unknown>): void {
  globalThis.fetch = (async (input: string | URL | Request) => {
    const url = String(input);
    requested.push(url);
    const body = slices[url];
    return body === undefined ? new Response('', { status: 503 }) : Response.json(body);
  }) as typeof fetch;
}

afterEach(() => {
  globalThis.fetch = realFetch;
  requested.length = 0;
});

describe('sliced messages', () => {
  test('a locale slice that fails to load renders the English inlined in the chunk, never the key', async () => {
    serve({});
    await registerMessages(`${CHUNKS}6.aaa.js`, { h: 'aaa', m: { page: { title: 'Commands' } } });
    expect(await ensureCatalog('fr')).toBe(false);
    expect(translate('fr', 'page.title')).toBe('Commands');
  });

  test('a loaded locale slice wins over the inlined English', async () => {
    serve({ 'https://dash.example/_app/immutable/i18n/fr/7.bbb.json': { quotes: { title: 'Citations' } } });
    await registerMessages(`${CHUNKS}7.bbb.js`, { h: 'bbb', m: { quotes: { title: 'Quotes' } } });
    await ensureCatalog('fr');
    expect(translate('fr', 'quotes.title')).toBe('Citations');
  });

  test('a slice carrying __proto__ cannot pollute Object.prototype', async () => {
    serve({ 'https://dash.example/_app/immutable/i18n/fr/9.ddd.json': JSON.parse('{"__proto__":{"polluted":"yes"},"poll":{"title":"Sondage"}}') });
    await registerMessages(`${CHUNKS}9.ddd.js`, { h: 'ddd', m: { poll: { title: 'Poll' } } });
    await ensureCatalog('fr');
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
    expect(translate('fr', 'poll.title')).toBe('Sondage');
  });

  test('a __proto__ locale cannot pollute Object.prototype', async () => {
    serve({ 'https://dash.example/_app/immutable/i18n/__proto__/9.ddd.json': { leaked: 'yes' } });
    await ensureCatalog('__proto__');
    expect(({} as Record<string, unknown>).leaked).toBeUndefined();
  });

  test('a chunk with no slice registers its English without fetching', async () => {
    serve({});
    await registerMessages(`${CHUNKS}8.ccc.js`, { h: '', m: {} });
    await ensureCatalog('fr');
    expect(requested.some((url) => url.endsWith('/8.ccc.json'))).toBe(false);
  });
});
