// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../../test/analytics';

stubSvelteKit();
const { RpcError } = await import('@bagel/kit/server/nats');
let status = 'free';
let prizes: { state: string }[] = [];
let basketError: Error | null = null;
let stateError = false;
let checkoutUrl: string | null = 'https://checkout.example/basket';
const requests: unknown[] = [];
const services = await import('../../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  billingState: async () => {
    if (stateError) throw new Error('state unavailable');
    return { status };
  },
  checkoutBasketCreate: async (request: unknown) => {
    requests.push(request);
    if (basketError) throw basketError;
    return { checkoutUrl };
  }
}));
const giveaways = await import('../../../lib/server/giveaways');
mock.module('#lib/server/giveaways', () => ({ ...giveaways, giveawayPrizes: async () => prizes }));
const { actions } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  status = 'free';
  prizes = [];
  basketError = null;
  stateError = false;
  checkoutUrl = 'https://checkout.example/basket';
  requests.length = 0;
});

const cases = [
  { action: 'subscribe', plan: 'monthly', result: 'redirected', http: 303 },
  { action: 'subscribe', plan: 'single', result: 'redirected', http: 303 },
  { action: 'subscribe', plan: 'monthly', status: 'paid', result: 'blocked', http: 409 },
  { action: 'subscribe', plan: 'single', prize: true, result: 'blocked', http: 409 },
  { action: 'subscribe', plan: 'monthly', stateError: true, result: 'rejected', http: 502 },
  { action: 'subscribe', plan: 'monthly', url: null, result: 'unavailable', http: 503 },
  { action: 'subscribe', plan: 'single', url: 'http://checkout.example', result: 'unavailable', http: 503 },
  { action: 'subscribe', plan: 'single', error: new RpcError('provider down', 'unavailable'), result: 'unavailable', http: 503 },
  { action: 'subscribe', plan: 'monthly', error: new RpcError('refused', 'invalid'), result: 'rejected', http: 503 },
  { action: 'subscribe', plan: 'monthly', error: new RpcError('that account already has premium coverage', 'conflict'), result: 'blocked', http: 503 },
  { action: 'subscribe', plan: 'single', error: new Error('unexpected'), result: 'rejected', http: 503 },
  { action: 'gift', plan: 'gift', result: 'redirected', http: 303 },
  { action: 'gift', plan: 'gift', recipient: '', result: 'rejected', http: 400 },
  { action: 'gift', plan: 'gift', recipient: 'bad recipient', result: 'rejected', http: 400 },
  { action: 'gift', plan: 'gift', message: 'https://example.com', result: 'rejected', http: 400 },
  { action: 'gift', plan: 'gift', error: new RpcError('refused', 'conflict'), result: 'rejected', http: 409 },
  { action: 'gift', plan: 'gift', error: new RpcError('provider down', 'unavailable'), result: 'unavailable', http: 409 },
  { action: 'gift', plan: 'gift', error: new RpcError('that user already has premium', 'invalid'), result: 'blocked', http: 409 },
  { action: 'gift', plan: 'gift', error: new RpcError('that account already has premium coverage', 'conflict'), result: 'blocked', http: 409 },
  { action: 'gift', plan: 'gift', error: new Error('unexpected'), result: 'rejected', http: 502 },
  { action: 'gift', plan: 'gift', url: null, result: 'unavailable', http: 502 }
];

test.each(cases)('$action $plan reports $result ($http)', async (row) => {
  status = row.status ?? 'free';
  prizes = row.prize ? [{ state: 'scheduled' }] : [];
  stateError = row.stateError ?? false;
  basketError = row.error ?? null;
  checkoutUrl = row.url === undefined ? checkoutUrl : row.url;
  const event = actionEvent({ plan: row.plan, recipient: row.recipient ?? 'recipient', message: row.message ?? '' });
  const response = await Promise.resolve().then(() => actions[row.action](event as never)).catch((err) => err);
  expect({ status: response.status, counts: await recorded() }).toEqual({
    status: row.http, counts: [[`billing.checkout|${row.plan}|${row.result}`, 1]]
  });
});

test.each(['subscribe', 'gift'])('%s rejects an unauthorized checkout once', async (action) => {
  const event = actionEvent({ plan: 'monthly' });
  const response = await actions[action]({ ...event, locals: { session: null, locale: 'en' } } as never);
  expect({ response, counts: await recorded(), requests }).toMatchObject({
    response: { status: 401 }, counts: [[`billing.checkout|${action === 'gift' ? 'gift' : 'monthly'}|rejected`, 1]], requests: []
  });
});

test('gift rejection preserves the submitted form payload', async () => {
  basketError = new RpcError('refused', 'conflict');
  const response = await actions.gift(actionEvent({ recipient: 'recipient', message: 'a gift' }) as never);
  expect(response).toEqual({ status: 409, data: { gift: true, error: 'refused', recipient: 'recipient', message: 'a gift' } });
});
