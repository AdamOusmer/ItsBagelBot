// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { allows } from '#lib/server/access';
import { ANALYTICS_DATA, ANALYTICS_WINDOWS, DEFAULT_ANALYTICS_WINDOW, buildAnalytics, type AnalyticsReads } from '#lib/analytics-page';
import { currentDay, windows, type AnalyticsRow, type Day } from '#lib/analytics-view';
import { buildDonations } from '#lib/donations-view';
import { donationsSummary } from '#lib/server/donations';
import { readAnalytics } from '#lib/server/analytics-reads';

function windowDays(raw: string | null): number {
  const n = Number(raw);
  return ANALYTICS_WINDOWS.find((d) => d === n) ?? DEFAULT_ANALYTICS_WINDOW;
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
  const analytics = readAnalytics(layout.id, days, end)
    .then((r) => ({ view: buildAnalytics(r), degraded: false }))
    .catch(() => ({ view: buildAnalytics(emptyReads(days, end)), degraded: true }));
  const donations = donationsSummary(layout.id, days)
    .then((reply) => ({ view: buildDonations(reply), degraded: false }))
    .catch(() => ({ view: buildDonations({}), degraded: true }));
  return { days, defaultDays: DEFAULT_ANALYTICS_WINDOW, windows: ANALYTICS_WINDOWS, period, analytics, donations };
};
