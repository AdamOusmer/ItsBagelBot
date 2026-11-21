// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import type { AnalyticsView } from '../src/lib/analytics-page';
import { addDays, currentDay, isoWeek, weekStart } from '../src/lib/analytics-view';
import type { DonationsReply, DonationsView } from '../src/lib/donations-view';

stubSvelteKit();
type Read = { actor: string; window: string; metrics: string[]; span: number };
const requests: Read[] = [];
const pendingDonations = Promise.withResolvers<DonationsReply>();
const today = currentDay(new Date());
const recentWeek = isoWeek(addDays(today, -110));
const lifecycleRows = [addDays(today, -140), weekStart(recentWeek), today]
  .map((start, index) => ({ start, metric: 'lifecycle', dims: ['signup'], value: index + 1 }));
const services = await import('../src/lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  analyticsSeries: async (actor: string, window: string, metrics: string[], span: number) => {
    if (actor === 'analytics-unavailable') throw new Error('Analytics unavailable');
    requests.push({ actor, window, metrics, span });
    return actor === 'wide-lifecycle' && metrics.includes('lifecycle') ? lifecycleRows : [];
  }
}));
mock.module('#lib/server/donations', () => ({
  donationsSummary: (actor: string) => actor === 'waiting-on-donations' ? pendingDonations.promise : Promise.resolve({})
}));
const { load } = await import('../src/routes/(admin)/analytics/+page.server');

type Stream<T> = Promise<{ view: T; degraded: boolean }>;
type Data = { analytics: Stream<AnalyticsView>; donations: Stream<DonationsView> };

async function loadPage(id: string, days: number): Promise<Data> {
  return await load({
    parent: async () => ({ id, role: 'owner' }),
    url: new URL(`https://admin.example/analytics?days=${days}`),
    depends: () => {}
  } as never) as Data;
}

test.each([
  { days: 1, counters: 93, lifecycle: [93, 120] },
  { days: 90, counters: 180, lifecycle: [180] },
  { days: 365, counters: 400, lifecycle: [400] }
])('reads the catalog with bounded RPC windows for $days days', async ({ days, counters, lifecycle }) => {
  const actor = `window-${days}`;
  const data = await loadPage(actor, days);
  const analytics = await data.analytics;
  const reads = requests.filter((r) => r.actor === actor);
  const extensions = [
    ['d', 'recipe.runs'], ['d', 'outgress.dropped'], ['d', 'provider.calls'], ['d', 'songs.requests'],
    ['d', 'command.blocked'], ['d', 'onboarding.steps'], ['d', 'import.runs'], ['d', 'dashboard.views'],
    ['d', 'billing.checkout'], ['d', 'recipes.total'], ['d', 'accounts.onboarded'], ['d', 'accounts.delegating'],
    ['w', 'recipe.channels'], ['w', 'activation.age'], ['d', 'modules.defaults'], ['w', 'channels.size'], ['m', 'channels.size'],
    ...['timers.ticks', 'timers.total', 'automod.flags', 'accounts.runtime', 'games.rounds',
      'games.entrants', 'points.flows', 'rewards.redeems', 'auth.login', 'signup.via', 'desktop.grants'].map((metric) => ['d', metric])
  ];
  expect({
    degraded: analytics.degraded,
    bounded: reads.every((r) => r.metrics.length <= 32),
    extensions: extensions.every(([window, metric]) => reads.some((r) => r.window === window && r.metrics.includes(metric))),
    dailyCoverage: reads.find((r) => r.metrics.includes('channels.active') && r.window === 'd')?.span,
    lifecycleCoverage: reads.filter((r) => r.metrics.includes('lifecycle')).map((r) => r.span)
  }).toEqual({ degraded: false, bounded: true, extensions: true, dailyCoverage: counters, lifecycleCoverage: [...lifecycle] });
});

test('wide analytics keeps older and live signups while retention uses recent cohorts', async () => {
  const data = await loadPage('wide-lifecycle', 365);
  const { view } = await data.analytics;
  expect({
    gained: view.growth.gainLoss.gained,
    currentDay: view.growth.signups.values.at(-1),
    cohorts: view.retention.rows.map((row) => row.week)
  }).toEqual({ gained: 6, currentDay: 3, cohorts: [recentWeek] });
});

test('analytics is ready while donations are pending and stays available after donations fail', async () => {
  const data = await loadPage('waiting-on-donations', 30);
  const analytics = await data.analytics;
  pendingDonations.reject(new Error('Donations unavailable'));
  expect([analytics.degraded, (await data.donations).degraded]).toEqual([false, true]);
});

test('donations remain available when analytics fails', async () => {
  const data = await loadPage('analytics-unavailable', 30);
  const [analytics, donations] = await Promise.all([data.analytics, data.donations]);
  expect([analytics.degraded, donations.degraded]).toEqual([true, false]);
});
