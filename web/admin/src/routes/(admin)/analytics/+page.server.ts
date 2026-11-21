// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows } from '#lib/server/access';
import { analyticsSeries, type AnalyticsWindow } from '#lib/server/services';
import { ANALYTICS_DATA, ANALYTICS_WINDOWS, DEFAULT_ANALYTICS_WINDOW, buildAnalytics, type AnalyticsReads } from '#lib/analytics-page';
import { addDays, currentDay, inRange, pick, windows, type AnalyticsRow, type Day } from '#lib/analytics-view';
import { buildDonations } from '#lib/donations-view';
import { donationsSummary } from '#lib/server/donations';

const DEMO = dev && process.env.DEMO === '1';
const COHORT_DAYS = 120;
const MAX_SPAN = 400;
const TWO_FULL_MONTHS = 93;

const COUNTERS = [
  'channels.active', 'channels.live', 'chat.messages', 'chat.answered', 'live.starts', 'timer.fires',
  'lifecycle', 'billing.events', 'onboarding.preset', 'command.uses', 'module.events', 'module.toggles',
  'custom.commands', 'recipe.runs', 'outgress.dropped', 'provider.calls', 'songs.requests',
  'command.blocked', 'onboarding.steps', 'import.runs', 'dashboard.views', 'billing.checkout',
  'timers.ticks', 'automod.flags', 'games.rounds', 'games.entrants', 'points.flows', 'rewards.redeems', 'auth.login', 'signup.via'
];
const LEVELS = [
  'accounts.total', 'accounts.bot_enabled', 'accounts.onboarded', 'accounts.paid_source',
  'accounts.cancel_pending', 'accounts.payment_failed', 'accounts.locale', 'accounts.pages_hidden',
  'accounts.by_signup_week', 'accounts.paid_by_signup_week', 'modules.rows', 'modules.defaults', 'modules.breadth',
  'integrations.connected', 'custom.total', 'custom.perm', 'custom.flags', 'custom.channels',
  'recipes.total', 'accounts.delegating', 'timers.total', 'accounts.runtime', 'accounts.inactive', 'desktop.grants'
];
const WEEKLY = ['channels.active', 'channels.live', 'module.active', 'command.channels', 'recipe.channels', 'activation.age', 'channels.size'];
const WEEK_SPAN = 28;
const MONTHLY = ['channels.active', 'channels.live', 'channels.size'];
const MONTH_SPAN = 70;

function windowDays(raw: string | null): number {
  const n = Number(raw);
  return ANALYTICS_WINDOWS.find((d) => d === n) ?? DEFAULT_ANALYTICS_WINDOW;
}

async function readAnalytics(actorId: string, days: number, end: Day): Promise<AnalyticsReads> {
  const counterSpan = Math.min(Math.max(2 * days, TWO_FULL_MONTHS), MAX_SPAN);
  const series = (w: AnalyticsWindow, metrics: string[], span: number) =>
    analyticsSeries(actorId, w, metrics, span);
  const dailyCounters = series('d', COUNTERS, counterSpan);
  const cohortLifecycle = counterSpan < COHORT_DAYS
    ? series('d', ['lifecycle'], COHORT_DAYS)
    : dailyCounters.then((rows) => inRange(pick(rows, 'lifecycle'), { from: addDays(end, -COHORT_DAYS), to: end }));
  const [counters, levels, week, month, cohortWeeks, cohortDays] = await Promise.all([
    dailyCounters,
    series('d', LEVELS, days),
    series('w', WEEKLY, WEEK_SPAN),
    series('m', MONTHLY, MONTH_SPAN),
    series('w', ['cohort.active'], COHORT_DAYS),
    cohortLifecycle
  ]);
  return { end, days, counters, levels, week, month, cohortWeeks, cohortDays };
}

const NONE: AnalyticsRow[] = [];
const emptyReads = (days: number, end: Day): AnalyticsReads => ({
  end,
  days,
  counters: NONE,
  levels: NONE,
  week: NONE,
  month: NONE,
  cohortWeeks: NONE,
  cohortDays: NONE
});

export const load: PageServerLoad = async ({ parent, url, depends }) => {
  depends(ANALYTICS_DATA);
  const layout = await parent();
  if (!allows(layout.role, 'analytics.read')) throw redirect(302, '/');
  const days = windowDays(url.searchParams.get('days'));
  const end = currentDay(new Date());
  const { cur: period } = windows(end, days);
  const reads = DEMO
    ? import('#lib/server/demo-data').then(({ demoAnalytics }) => demoAnalytics(days))
    : readAnalytics(layout.id, days, end);
  const ledger = DEMO
    ? import('#lib/server/demo-data').then(({ demoDonations }) => demoDonations(days))
    : donationsSummary(layout.id, days);
  const analytics = reads
    .then((r) => ({ view: buildAnalytics(r), degraded: false }))
    .catch(() => ({ view: buildAnalytics(emptyReads(days, end)), degraded: true }));
  const donations = ledger
    .then((reply) => ({ view: buildDonations(reply), degraded: false }))
    .catch(() => ({ view: buildDonations({}), degraded: true }));
  return { days, defaultDays: DEFAULT_ANALYTICS_WINDOW, windows: ANALYTICS_WINDOWS, period, analytics, donations };
};
