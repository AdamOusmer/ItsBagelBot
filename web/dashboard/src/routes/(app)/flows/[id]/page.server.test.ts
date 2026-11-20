// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { staticText } from '../../../../../../kit/lib/i18n/static';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit();

let allowed = true;
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class { async check() { return { allowed }; } }
}));

const fetches = await import('#lib/server/fetches-store');
const sample = '{"city":"London"}';
const rehearseFetch = mock<typeof fetches.rehearseFetch>();
mock.module('#lib/server/fetches-store', () => ({ ...fetches, rehearseFetch }));

const { actions } = await import('./+page.server');
const locals = { session: { user_id: '7' }, locale: 'en', accountState: { value: { status: 'paid' } } };
const address = 'https://example.com/data';
const draft = { name: 'draft', url: address, jsonPath: [], keyLabel: '' };

test.each([
  {
    name: 'returns the JSON sample for an inline HTTPS address',
    url: address,
    expected: { ok: true, action: 'fetchtested', status: 'ok', sample },
    fetched: true
  },
  {
    name: 'rejects an invalid address before fetching',
    url: 'http://example.com/data',
    expected: { status: 400, data: { ok: false, error: staticText('en', 'validation.urlHttps') } },
    fetched: false
  },
  {
    name: 'returns a rate limit error without fetching',
    url: address,
    throttled: true,
    expected: { status: 429, data: { ok: false, error: staticText('en', 'serverErrors.fetchRate') } },
    fetched: false
  },
  {
    name: 'returns a service error when the fetch service is unavailable',
    url: address,
    unavailable: true,
    expected: { status: 502, data: { ok: false, error: staticText('en', 'serverErrors.fetchService') } },
    fetched: true
  }
])('$name', async ({ url, expected, fetched, throttled, unavailable }) => {
  allowed = !throttled;
  rehearseFetch.mockReset();
  if (unavailable) rehearseFetch.mockRejectedValue(new Error('Service unavailable'));
  else rehearseFetch.mockResolvedValue({ status: 'ok', values: [], ms: 1, sample });
  const body = new FormData();
  body.set('kind', 'json');
  body.set('url', url);
  body.set('path', '');
  const request = new Request('https://dashboard.itsbagelbot.com/flows/new?/testfetch', { method: 'POST', body });
  expect(await actions.testfetch({ request, locals } as never)).toMatchObject(expected);
  expect(rehearseFetch.mock.calls).toEqual(fetched ? [['7', draft]] : []);
});
