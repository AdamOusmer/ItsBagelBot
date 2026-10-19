// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { allows } from '$lib/server/access';
import { analyticsSeries, type AnalyticsWindow } from '$lib/server/services';
import { ANALYTICS_WINDOWS, buildAnalytics, type AnalyticsReads, type AnalyticsView } from '$lib/analytics-page';
import type { AnalyticsRow } from '$lib/analytics-view';

const DEMO = dev && process.env.DEMO === '1';
const COHORT_DAYS = 120;

const DAILY = [
  'channels.active', 'chat.messages', 'chat.answered', 'live.starts', 'channels.live', 'lifecycle',
  'billing.events', 'accounts.total', 'accounts.bot_enabled', 'accounts.onboarded', 'accounts.paid_source',
  'accounts.cancel_pending', 'accounts.payment_failed', 'accounts.locale', 'accounts.pages_hidden',
  'command.uses', 'module.events', 'module.errors', 'module.latency', 'timer.fires',
  'discord.interactions', 'discord.events', 'outgress.failures', 'onboarding.preset', 'module.toggles',
  'custom.commands', 'web.views', 'web.actions'
];
const WEEKLY = ['channels.active', 'module.active', 'channels.live'];
const MONTHLY = ['channels.active', 'channels.live'];

function windowDays(raw: string | null): number {
  const n = Number(raw);
  return ANALYTICS_WINDOWS.find((d) => d === n) ?? ANALYTICS_WINDOWS[0];
}

async function read(actorId: string, days: number): Promise<AnalyticsReads> {
  const series = (w: AnalyticsWindow, metrics: string[], span: number) =>
    analyticsSeries(actorId, w, metrics, span);
  const [day, week, month, cohortWeeks, cohortDays] = await Promise.all([
    series('d', DAILY, days),
    series('w', WEEKLY, days),
    series('m', MONTHLY, 400),
    series('w', ['cohort.active'], COHORT_DAYS),
    series('d', ['lifecycle'], COHORT_DAYS)
  ]);
  return { day, week, month, cohortWeeks, cohortDays };
}

const NONE: AnalyticsRow[] = [];
const EMPTY_READS: AnalyticsReads = { day: NONE, week: NONE, month: NONE, cohortWeeks: NONE, cohortDays: NONE };

export const load: PageServerLoad = async ({ parent, url }) => {
  const layout = await parent();
  if (!allows(layout.role, 'analytics.read')) throw redirect(302, '/');
  const days = windowDays(url.searchParams.get('days'));
  const reads = DEMO
    ? import('$lib/server/demo-data').then(({ demoAnalytics }) => demoAnalytics(days))
    : read(layout.id, days);
  const bundle: Promise<{ view: AnalyticsView; degraded: boolean }> = reads
    .then((r) => ({ view: buildAnalytics(r), degraded: false }))
    .catch(() => ({ view: buildAnalytics(EMPTY_READS), degraded: true }));
  return { days: days as number, windows: [...ANALYTICS_WINDOWS] as number[], bundle };
};
