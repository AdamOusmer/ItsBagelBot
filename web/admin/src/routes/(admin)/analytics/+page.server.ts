// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows } from '#lib/server/access';
import { analyticsSeries, type AnalyticsWindow } from '#lib/server/services';
import { ANALYTICS_WINDOWS, DEFAULT_ANALYTICS_WINDOW, buildAnalytics, type AnalyticsReads, type AnalyticsView } from '#lib/analytics-page';
import { lastClosedDay, type AnalyticsRow } from '#lib/analytics-view';

const DEMO = dev && process.env.DEMO === '1';
const COHORT_DAYS = 120;
const MAX_SPAN = 400;

const COUNTERS = [
  'channels.active', 'channels.live', 'chat.messages', 'chat.answered', 'live.starts', 'timer.fires',
  'lifecycle', 'billing.events', 'onboarding.preset', 'command.uses', 'module.events', 'module.toggles',
  'custom.commands'
];
const LEVELS = [
  'accounts.total', 'accounts.bot_enabled', 'accounts.onboarded', 'accounts.paid_source',
  'accounts.cancel_pending', 'accounts.payment_failed', 'accounts.locale', 'accounts.pages_hidden',
  'accounts.by_signup_week', 'accounts.paid_by_signup_week', 'modules.rows', 'modules.breadth',
  'integrations.connected', 'custom.total', 'custom.perm', 'custom.flags', 'custom.channels'
];
const WEEKLY = ['channels.active', 'channels.live', 'module.active', 'command.channels'];
const WEEK_SPAN = 28;
const MONTHLY = ['channels.active', 'channels.live'];
const MONTH_SPAN = 70;

function windowDays(raw: string | null): number {
  const n = Number(raw);
  return ANALYTICS_WINDOWS.find((d) => d === n) ?? DEFAULT_ANALYTICS_WINDOW;
}

async function read(actorId: string, days: number): Promise<AnalyticsReads> {
  const series = (w: AnalyticsWindow, metrics: string[], span: number) =>
    analyticsSeries(actorId, w, metrics, span);
  const [counters, levels, week, month, cohortWeeks, cohortDays] = await Promise.all([
    series('d', COUNTERS, Math.min(2 * days, MAX_SPAN)),
    series('d', LEVELS, days + 1),
    series('w', WEEKLY, WEEK_SPAN),
    series('m', MONTHLY, MONTH_SPAN),
    series('w', ['cohort.active'], COHORT_DAYS),
    series('d', ['lifecycle'], COHORT_DAYS)
  ]);
  return { end: lastClosedDay(new Date()), days, counters, levels, week, month, cohortWeeks, cohortDays };
}

const NONE: AnalyticsRow[] = [];
const emptyReads = (days: number): AnalyticsReads => ({
  end: lastClosedDay(new Date()),
  days,
  counters: NONE,
  levels: NONE,
  week: NONE,
  month: NONE,
  cohortWeeks: NONE,
  cohortDays: NONE
});

export const load: PageServerLoad = async ({ parent, url }) => {
  const layout = await parent();
  if (!allows(layout.role, 'analytics.read')) throw redirect(302, '/');
  const days = windowDays(url.searchParams.get('days'));
  const reads = DEMO
    ? import('#lib/server/demo-data').then(({ demoAnalytics }) => demoAnalytics(days))
    : read(layout.id, days);
  const bundle: Promise<{ view: AnalyticsView; degraded: boolean }> = reads
    .then((r) => ({ view: buildAnalytics(r), degraded: false }))
    .catch(() => ({ view: buildAnalytics(emptyReads(days)), degraded: true }));
  return { days: days as number, defaultDays: DEFAULT_ANALYTICS_WINDOW, windows: [...ANALYTICS_WINDOWS] as number[], bundle };
};
