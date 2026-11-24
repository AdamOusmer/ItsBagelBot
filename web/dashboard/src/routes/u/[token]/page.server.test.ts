// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';
import { installMarketingRpc, resetMarketingRpc, rpcCalls, rpcState } from '../../../../test/marketing-rpc';

stubSvelteKit();
await installMarketingRpc();
const { actions, load } = await import('./+page.server');

const token = 'v1.7.1700000000000000.marketing.mac';
const origin = 'https://dashboard.itsbagelbot.com';
const getSubject = 'rpc.dashboard.marketing_token_get';
const setSubject = 'rpc.dashboard.marketing_token_set';

beforeEach(resetMarketingRpc);

function submit(fields: object, headers: object = { origin }) {
  const request = new Request(`${origin}/u/${token}`, { method: 'POST', headers: headers as Record<string, string>, body: new URLSearchParams(fields as Record<string, string>) });
  return actions.default({ request, params: { token }, url: new URL(request.url), locals: { locale: 'en' } } as never);
}

test.each([
  { status: 'granted', want: { state: 'ready', status: 'granted', maskedEmail: 'c***@example.test' } },
  { status: 'withdrawn', want: { state: 'ready', status: 'withdrawn', maskedEmail: 'c***@example.test' } }
])('GET renders the $status state and never writes', async ({ status, want }) => {
  rpcState.reply = { ok: true, status, masked_email: 'c***@example.test', locale: 'en' };
  const headers: Record<string, string> = {};
  const result = await load({ params: { token }, setHeaders: (h: Record<string, string>) => Object.assign(headers, h) } as never);
  expect(result).toEqual({ view: want });
  expect(headers).toEqual({ 'cache-control': 'no-store', 'referrer-policy': 'no-referrer' });
  expect(rpcCalls).toEqual([[getSubject, { token }]]);
});

test.each([
  { refusal: 'invalid', state: 'invalid' },
  { refusal: 'unavailable', state: 'unavailable' }
])('GET with a $refusal Users reply shows the $state view', async ({ refusal, state }) => {
  rpcState.refusal = refusal;
  const result = await load({ params: { token }, setHeaders: () => {} } as never);
  expect(result).toEqual({ view: { state } });
  expect(rpcCalls.map(([subject]) => subject)).toEqual([getSubject]);
});

test.each([
  { name: 'unsubscribe button', fields: { status: 'withdrawn' }, write: { status: 'withdrawn', source: 'preference_page' } },
  { name: 'one-click form with no Origin', fields: { 'List-Unsubscribe': 'One-Click' }, headers: {}, write: { status: 'withdrawn', source: 'one_click' } }
])('POST $name', async ({ fields, headers, write }) => {
  expect(await submit(fields, headers)).toEqual({ ok: true });
  expect(rpcCalls).toEqual([[setSubject, { token, ...write }]]);
});

test.each([
  { name: 'a resubscribe, which needs a signed-in Settings change', fields: { status: 'granted' }, status: 400 },
  { name: 'a cross-site unsubscribe', fields: { status: 'withdrawn' }, headers: { origin: 'https://example.test' }, status: 403 },
  { name: 'a preference POST with no Origin', fields: { status: 'withdrawn' }, headers: {}, status: 403 },
  { name: 'an unknown status', fields: { status: 'none' }, status: 400 },
  { name: 'a missing status', fields: {}, status: 400 }
])('refuses $name without calling Users', async ({ fields, headers, status }) => {
  expect(await submit(fields, headers)).toMatchObject({ status });
  expect(rpcCalls).toEqual([]);
});

test.each([
  { refusal: 'invalid', status: 404 },
  { refusal: 'unavailable', status: 502 }
])('a $refusal write reply surfaces as $status', async ({ refusal, status }) => {
  rpcState.refusal = refusal;
  expect(await submit({ status: 'withdrawn' })).toMatchObject({ status });
});
