// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, test } from 'bun:test';
import { count, flushAnalytics, setAnalyticsClientForTests } from '@bagel/kit/server/analytics';
import { MARKETING_CTAS, PUBLIC_PAGES } from '@bagel/kit/analytics-beacon-catalog';
import { POST } from './+server';

afterEach(() => setAnalyticsClientForTests(undefined));

function aggregate() {
  const fields: Record<string, number> = {};
  const pipe = {
    hincrby: (_key: string, field: string, value: number) => { fields[field] = value; return pipe; },
    expireat: () => pipe,
    exec: async () => []
  };
  setAnalyticsClientForTests({ pipeline: () => pipe });
  return fields;
}

async function post(body: string): Promise<Response> {
  const request = { json: () => Promise.resolve().then(() => JSON.parse(body)) };
  const event = new Proxy({ request }, {
    get(target, key) {
      if (key === 'request') return target.request;
      throw new Error(`Unexpected request context read: ${String(key)}`);
    }
  });
  return POST(event as never);
}

test('counts only catalog values and ignores extra fields without reading request metadata or setting cookies', async () => {
  const fields = aggregate();
  const events = [
    ...PUBLIC_PAGES.map((dim) => ({ event: 'public.views', dim })),
    ...MARKETING_CTAS.map((dim) => ({ event: 'marketing.cta', dim }))
  ];
  for (const event of events) {
    const response = await post(JSON.stringify({ ...event, user_id: '123', url: '/user/private', ip: '192.0.2.1', headers: { cookie: 'private' } }));
    expect({ status: response.status, headers: [...response.headers], body: await response.text() })
      .toEqual({ status: 204, headers: [['cache-control', 'no-store']], body: '' });
  }
  await flushAnalytics();
  expect(fields).toEqual(Object.fromEntries(events.map(({ event, dim }) => [`${event}|${dim}`, 1])));
});

test.each([
  'invalid', 'null', '[]', '1', '"public.views"', '{}',
  '{"event":"public.views","dim":"streamer-login"}',
  '{"event":"marketing.cta","dim":"123456789"}',
  '{"event":"public.views","dim":["commands"]}',
  '{"event":"public.views","dim":{"page":"commands"}}',
  '{"event":"dashboard.views","dim":"commands"}',
  '{"event":"__proto__","dim":"commands"}'
])('silently drops invalid payload %s', async (body) => {
  const fields = aggregate();
  const response = await post(body);
  await flushAnalytics();
  expect({ status: response.status, headers: [...response.headers], fields })
    .toEqual({ status: 204, headers: [['cache-control', 'no-store']], fields: {} });
});

test('public endpoint cannot record other server counters', async () => {
  const fields = aggregate();
  await post('{"event":"auth.login","dim":"ok"}');
  count('auth.login', 'ok');
  await flushAnalytics();
  expect(fields).toEqual({ 'auth.login|ok': 1 });
});
