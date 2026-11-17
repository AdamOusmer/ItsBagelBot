// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
let saveFails = false;
const saved: unknown[] = [];
const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  setOnboarded: async (...args: unknown[]) => {
    if (saveFails) throw new Error('save failed');
    saved.push(args);
  }
}));
mock.module('#lib/server/feature-presets', () => ({ applyFeaturePreset: async () => {} }));
const { actions, load } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  saveFails = false;
  saved.length = 0;
});

test('counts the welcome view once', async () => {
  await load(actionEvent() as never);
  expect(await recorded()).toEqual([['onboarding.steps|viewed', 1]]);
});

test.each([
  { action: 'done', consent: '', failed: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'finishImport', consent: '', failed: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'done', consent: 'yes', failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'finishImport', consent: 'yes', failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'done', consent: 'yes', failed: false, status: 303, counts: [['onboarding.preset|quiet', 1]] },
  { action: 'finishImport', consent: 'yes', failed: false, status: undefined, counts: [['onboarding.preset|import', 1]] }
])('$action consent=$consent failed=$failed', async ({ action, consent, failed, status, counts }) => {
  saveFails = failed;
  const response = await Promise.resolve().then(() => actions[action](actionEvent({ consent, preset: 'quiet' }) as never)).catch((err) => err);
  expect({ status: response.status, counts: await recorded() }).toEqual({ status, counts: counts.map(([field, n]) => [field, n]) });
});
