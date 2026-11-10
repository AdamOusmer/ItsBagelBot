// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, it } from 'bun:test';
import { count, flushAnalytics, setAnalyticsClientForTests } from './analytics';

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

afterEach(() => setAnalyticsClientForTests(undefined));

describe('analytics counters', () => {
  it('sums catalog fields into the day hash and expires it a week after the day', async () => {
    const { calls, client } = fakeClient();
    setAnalyticsClientForTests(client);

    count('onboarding.preset', 'quiet');
    count('module.toggles', 'quotes', 'on');
    count('module.toggles', 'quotes', 'on');
    count('custom.commands', 'created');
    await flushAnalytics(day);

    expect(calls).toEqual([
      ['hincrby', 'an:c:20300314', 'onboarding.preset|quiet', 1],
      ['hincrby', 'an:c:20300314', 'module.toggles|quotes|on', 2],
      ['hincrby', 'an:c:20300314', 'custom.commands|created', 1],
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
    count('web.views', 'dashboard', '/(app)/commands');
    await flushAnalytics(day);

    expect(calls).toEqual([]);
  });
});
