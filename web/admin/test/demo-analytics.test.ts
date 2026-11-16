// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, it } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import { buildAnalytics } from '../src/lib/analytics-page';

stubSvelteKit({ dev: true });
const { demoAnalytics } = await import('../src/lib/server/demo-data');

it.each([1, 7, 30, 90, 365])('builds demo analytics for a %s day window', (days) => {
  const reads = demoAnalytics(days);
  const view = buildAnalytics(reads);
  expect({
    days: reads.days,
    recipes: view.recipes.runs.some((bar) => bar.value > 0),
    channels: Number(view.recipes.channels.value) > 0,
    drops: view.reliability.dailyDrops.values.length,
    providers: view.reliability.providers.rows.length > 0,
    ages: view.growth.activationAge.some((bar) => bar.value > 0),
    dashboard: view.features.dashboard.some((bar) => bar.value > 0),
    checkout: view.revenue.checkout.rows.length,
    delegating: view.audience.delegating[0].value !== '0',
    sizes: [reads.week, reads.month].every((rows) => rows.some((row) => row.metric === 'channels.size')),
    roadmap: [view.community.ticks, view.community.timers, view.community.points, view.audience.runtime,
      view.audience.desktop, view.growth.login, view.growth.cta, view.audience.publicViews].every((bars) => bars.some((b) => b.value > 0)),
    promotions: view.growth.trials[0].value !== '0'
  }).toEqual({ days, recipes: true, channels: true, drops: days, providers: true, ages: true, dashboard: true, checkout: 3, delegating: true, sizes: true, roadmap: true, promotions: true });
});
