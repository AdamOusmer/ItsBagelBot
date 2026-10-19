// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, it } from 'bun:test';
import { count, countAction, countView, flushAnalytics, setAnalyticsApp, setAnalyticsClientForTests } from './analytics';

type Call = [string, ...unknown[]];

function fakeClient() {
  const calls: Call[] = [];
  const pipe = {
    hincrby: (...args: unknown[]) => (calls.push(['hincrby', ...args]), pipe),
    expireat: (...args: unknown[]) => (calls.push(['expireat', ...args]), pipe),
    exec: async () => calls
  };
  return { calls, client: { pipeline: () => pipe } };
}

const day = new Date(Date.UTC(2030, 2, 14, 15, 0, 0));
const page = (route: string | null, method = 'GET') => ({
  request: new Request('https://dash.test/', { method, headers: { accept: 'text/html' } }),
  route: { id: route }
});

afterEach(() => setAnalyticsClientForTests(undefined));

describe('analytics counters', () => {
  it('sums catalog fields into the day hash and expires it a week after the day', async () => {
    const { calls, client } = fakeClient();
    setAnalyticsClientForTests(client);
    setAnalyticsApp('dashboard');

    countView(page('/(app)/commands'));
    countView(page('/(app)/commands'));
    countView(page('/(app)/commands', 'POST'));
    countView(page(null));
    countAction({ route: { id: '/(app)/timers' }, url: new URL('https://dash.test/timers?/toggle') }, 'ok');
    count('module.toggles', 'quotes', 'on');
    await flushAnalytics(day);

    expect(calls).toEqual([
      ['hincrby', 'an:c:20300314', 'web.views|dashboard|/(app)/commands', 2],
      ['hincrby', 'an:c:20300314', 'web.actions|dashboard|/(app)/timers?/toggle|ok', 1],
      ['hincrby', 'an:c:20300314', 'module.toggles|quotes|on', 1],
      ['expireat', 'an:c:20300314', Date.UTC(2030, 2, 15) / 1000 + 7 * 24 * 3600]
    ]);
  });

  it('drops values outside the catalog, so free text never reaches Valkey', async () => {
    const { calls, client } = fakeClient();
    setAnalyticsClientForTests(client);

    count('module.toggles', 'My Cool Module!', 'on');
    count('custom.commands', 'renamed');
    count('viewer.logins', 'mavey');
    count('onboarding.preset');
    await flushAnalytics(day);

    expect(calls).toEqual([]);
  });

  it('records nothing before the app names itself', async () => {
    const { calls, client } = fakeClient();
    setAnalyticsClientForTests(client);

    countView(page('/(app)/commands'));
    await flushAnalytics(day);

    expect(calls).toEqual([]);
  });
});
