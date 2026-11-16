// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { publicPageForRoute, usagePayload } from './analytics-beacon';
import { MARKETING_CTAS, PUBLIC_PAGES, type BeaconEvent } from './analytics-beacon-catalog';

test.each([
  ['/(public)/user/[channel]', 'commands'], ['/(public)/[user]', 'leaderboard'],
  ['/(public)/queue/[user]', 'queue'], ['/(public)/sr/[user]', 'songqueue'],
  ['/(public)/stats', 'stats'], ['/(public)/login', 'login'],
  [null, undefined], ['/(app)/commands', undefined], ['/user/private-login', undefined], ['toString', undefined]
])('maps route %s to a closed page enum', (route, expected) => {
  expect(publicPageForRoute(route)).toBe(expected);
});

test.each([
  ...PUBLIC_PAGES.map((dim) => ({ event: 'public.views', dim } as const)),
  ...MARKETING_CTAS.map((dim) => ({ event: 'marketing.cta', dim } as const))
])('serializes only $event and $dim without private extras', (event) => {
  expect(usagePayload({ ...event, user_id: '123', referrer: '/user/private', url: '?channel=private' } as BeaconEvent))
    .toBe(JSON.stringify(event));
});

test.each([
  { event: 'public.views', dim: 'streamer-login' },
  { event: 'marketing.cta', dim: '123456789' },
  { event: 'dashboard.views', dim: 'commands' },
  { event: 'public.views', dim: ['commands'] },
  { event: 'public.views', dim: 'commands|123' },
  { event: '__proto__', dim: 'commands' }
])('drops invalid client event $event with $dim', (input) => {
  expect(usagePayload(input as BeaconEvent)).toBeNull();
});

test('keeps the browser enums aligned with the Go aggregate catalog', () => {
  const go = readFileSync(new URL('../../../internal/domain/analytics/catalog.go', import.meta.url), 'utf8');
  const enums = [
    { kind: 'KindPage', values: PUBLIC_PAGES },
    { kind: 'KindCTA', values: MARKETING_CTAS }
  ];
  for (const { kind, values } of enums) {
    const body = go.match(new RegExp(`${kind}:\\s*\\{([^}]+)\\}`))?.[1];
    expect(body?.match(/"([^"]+)"/g)?.map((value) => JSON.parse(value))).toEqual([...values]);
  }
});
