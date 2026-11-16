// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, it, setSystemTime } from 'bun:test';
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

beforeEach(() => setSystemTime(day));
afterEach(() => {
  setAnalyticsClientForTests(undefined);
  setSystemTime();
});

describe('analytics counters', () => {
  it('sums catalog fields into the day hash and expires it a week after the day', async () => {
    const { calls, client } = fakeClient();
    setAnalyticsClientForTests(client);

    count('onboarding.preset', 'quiet');
    count('module.toggles', 'quotes', 'on');
    count('module.toggles', 'quotes', 'on');
    count('custom.commands', 'created');
    await flushAnalytics();

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
    await flushAnalytics();

    expect(calls).toEqual([]);
  });
});

it('keeps identical fields on their event days across a UTC year boundary and delayed flush', async () => {
  const { calls, client } = fakeClient();
  setAnalyticsClientForTests(client);
  setSystemTime(new Date('2030-12-31T23:59:59.999Z'));
  count('onboarding.preset', 'quiet');
  count('onboarding.preset', 'quiet');
  setSystemTime(new Date('2031-01-01T00:00:00.000Z'));
  count('onboarding.preset', 'quiet');
  setSystemTime(new Date('2031-01-03T12:00:00.000Z'));
  await flushAnalytics();
  await flushAnalytics();
  expect(calls).toEqual([
    ['hincrby', 'an:c:20301231', 'onboarding.preset|quiet', 2],
    ['hincrby', 'an:c:20310101', 'onboarding.preset|quiet', 1],
    ['expireat', 'an:c:20301231', Date.UTC(2031, 0, 8) / 1000],
    ['expireat', 'an:c:20310101', Date.UTC(2031, 0, 9) / 1000]
  ]);
});

it('buffers every valid field without a fixed slot cap', async () => {
  const { calls, client } = fakeClient();
  setAnalyticsClientForTests(client);
  for (let i = 0; i < 2050; i++) count('module.toggles', `module${i}`, 'on');
  await flushAnalytics();
  expect(calls.filter(([op]) => op === 'hincrby')).toHaveLength(2050);
});

const metrics = [
  { metric: 'auth.login', dims: [['started', 'ok', 'declined', 'oauth', 'state', 'bot', 'scope', 'link', 'retry', 'banned']] },
  { metric: 'dashboard.views', dims: [
    ['overview', 'commands', 'flows', 'automations', 'modules', 'timers', 'loyalty', 'counters',
      'quotes', 'songqueue', 'channelpoints', 'govee', 'billing', 'settings', 'access'], ['owner', 'delegate']
  ] },
  { metric: 'billing.checkout', dims: [['monthly', 'single', 'gift'], ['redirected', 'blocked', 'unavailable', 'rejected']] },
  { metric: 'import.runs', dims: [['streamelements', 'fossabot', 'moobot', 'nightbot', 'streamlabs_desktop', 'wizebot'],
    ['preview_ok', 'preview_failed', 'committed', 'commit_failed']] },
  { metric: 'onboarding.steps', dims: [['viewed', 'consent_missing', 'save_failed']] }
];

it.each(metrics)('accepts every catalog enum combination for $metric', async ({ metric, dims }) => {
  const { calls, client } = fakeClient();
  setAnalyticsClientForTests(client);
  const combinations = dims.reduce<string[][]>((rows, values) => rows.flatMap((row) => values.map((v) => [...row, v])), [[]]);
  for (const row of combinations) count(metric, ...row);
  await flushAnalytics();
  expect(calls.filter(([op]) => op === 'hincrby')).toEqual(
    combinations.map((row) => ['hincrby', 'an:c:20300314', [metric, ...row].join('|'), 1])
  );
});

it.each(metrics)('rejects private or malformed dimensions for $metric', async ({ metric, dims }) => {
  const { calls, client } = fakeClient();
  setAnalyticsClientForTests(client);
  const row = dims.map((values) => values[0]);
  for (const index of row.keys()) {
    for (const value of ['123456789', 'streamer-login', 'owner|delegate', 'unknown', '']) {
      count(metric, ...row.map((v, i) => i === index ? value : v));
    }
  }
  count(metric, ...row.slice(1));
  count(metric, ...row, 'extra');
  await flushAnalytics();
  expect(calls).toEqual([]);
});
