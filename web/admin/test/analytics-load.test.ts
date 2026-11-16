// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import type { AnalyticsView } from '../src/lib/analytics-page';

stubSvelteKit();
type Read = { window: string; metrics: string[]; span: number };
const requests: Read[] = [];
const services = await import('../src/lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  analyticsSeries: async (_actor: string, window: string, metrics: string[], span: number) => {
    requests.push({ window, metrics, span });
    return [];
  }
}));
const { load } = await import('../src/routes/(admin)/analytics/+page.server');

test('reads every extension in its catalog window within the RPC metric limit', async () => {
  const data = await load({
    parent: async () => ({ id: 'admin', role: 'owner' }),
    url: new URL('https://admin.example/analytics?days=1'),
    depends: () => {}
  } as never) as { bundle: Promise<{ view: AnalyticsView; degraded: boolean }> };
  const bundle = await data.bundle;
  const extensions = [
    ['d', 'recipe.runs'], ['d', 'outgress.dropped'], ['d', 'provider.calls'], ['d', 'songs.requests'],
    ['d', 'command.blocked'], ['d', 'onboarding.steps'], ['d', 'import.runs'], ['d', 'dashboard.views'],
    ['d', 'billing.checkout'], ['d', 'recipes.total'], ['d', 'accounts.onboarded'], ['d', 'accounts.delegating'],
    ['w', 'recipe.channels'], ['w', 'activation.age']
  ];
  expect({
    degraded: bundle.degraded,
    bounded: requests.every((r) => r.metrics.length <= 32),
    extensions: extensions.every(([window, metric]) => requests.some((r) => r.window === window && r.metrics.includes(metric))),
    dailyCoverage: requests.find((r) => r.metrics.includes('channels.active') && r.window === 'd')?.span
  }).toEqual({ degraded: false, bounded: true, extensions: true, dailyCoverage: 93 });
});
