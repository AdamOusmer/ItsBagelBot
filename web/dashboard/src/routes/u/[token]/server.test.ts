// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';
import { installMarketingRpc, resetMarketingRpc, rpcCalls, rpcState } from '../../../../test/marketing-rpc';

stubSvelteKit();
await installMarketingRpc();
const route = await import('./+server');

const token = 'v1.7.1700000000000000.marketing.mac';
const setSubject = 'rpc.dashboard.marketing_token_set';
const oneClick = 'List-Unsubscribe=One-Click';

function post(body: string, type = 'application/x-www-form-urlencoded') {
  const request = new Request(`https://dashboard.itsbagelbot.com/u/${token}`, { method: 'POST', headers: { 'content-type': type }, body });
  return route.POST({ request, params: { token }, locals: {} } as never);
}

beforeEach(resetMarketingRpc);

test('the endpoint answers POST only, so a GET can never unsubscribe', () => {
  expect(Object.keys(route)).toEqual(['POST']);
});

test('one-click POST withdraws consent without a session and replies 200 text/plain', async () => {
  const res = await post(oneClick);
  expect({ status: res.status, type: res.headers.get('content-type'), cookie: res.headers.get('set-cookie'), location: res.headers.get('location') }).toEqual({
    status: 200, type: 'text/plain; charset=utf-8', cookie: null, location: null
  });
  expect(rpcCalls).toEqual([[setSubject, { token, status: 'withdrawn', source: 'one_click' }]]);
});

test.each([
  { name: 'missing one-click field', body: 'other=1', type: undefined },
  { name: 'wrong one-click value', body: 'List-Unsubscribe=Maybe', type: undefined },
  { name: 'JSON body', body: oneClick, type: 'application/json' },
  { name: 'oversized body', body: `${oneClick}&pad=${'x'.repeat(2048)}`, type: undefined }
])('rejects $name without calling Users', async ({ body, type }) => {
  expect((await post(body, type)).status).toBe(400);
  expect(rpcCalls).toEqual([]);
});

test.each([
  { name: 'a token Users refuses', refusal: 'invalid', status: 404 },
  { name: 'a Users outage', refusal: 'unavailable', status: 503 }
])('maps $name to $status', async ({ refusal, status }) => {
  rpcState.refusal = refusal;
  expect((await post(oneClick)).status).toBe(status);
});

test('a refused write reply is a failure, not a 200', async () => {
  rpcState.reply = { ok: false };
  expect((await post(oneClick)).status).toBe(503);
});
