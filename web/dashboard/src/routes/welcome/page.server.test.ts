// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
let saveFails = false;
const saved: unknown[] = [];
const published: unknown[] = [];
const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  setOnboarded: async (...args: unknown[]) => {
    if (saveFails) throw new Error('save failed');
    saved.push(args);
  },
  publishEventSub: async (...args: unknown[]) => {
    published.push(args);
  }
}));
mock.module('#lib/server/feature-presets', () => ({ applyFeaturePreset: async () => {} }));
const { actions, load } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  saveFails = false;
  saved.length = 0;
  published.length = 0;
});

test('counts the welcome view once', async () => {
  await load(actionEvent() as never);
  expect(await recorded()).toEqual([['onboarding.steps|viewed', 1]]);
});

test.each([
  { name: 'owner without state', session: {}, accountState: undefined, want: { landing: null, accepted: false } },
  { name: 'unaccepted owner', session: {}, accountState: { value: { onboarded: false } }, want: { landing: null, accepted: false } },
  { name: 'accepted owner', session: {}, accountState: { value: { onboarded: true } }, want: { landing: null, accepted: true } },
  { name: 'unaccepted delegate', session: { delegate_of: '2' }, accountState: { value: { onboarded: false } }, want: { landing: null, accepted: false } },
  { name: 'accepted delegate', session: { delegate_of: '2' }, accountState: { value: { onboarded: true } }, want: { landing: '/', accepted: undefined } },
  { name: 'impersonation', session: { impersonator_id: '3' }, accountState: { value: { onboarded: false } }, want: { landing: '/', accepted: undefined } }
])('load for $name', async ({ session, accountState, want }) => {
  const event = actionEvent() as { locals: Record<string, unknown> & { session: Record<string, unknown> } };
  Object.assign(event.locals.session, session);
  event.locals.accountState = accountState;
  const got = await Promise.resolve(load(event as never)).then(
    (data) => ({ landing: null, accepted: (data as { accepted: boolean }).accepted }),
    (e: { location?: string }) => ({ landing: e.location ?? null, accepted: undefined })
  );
  expect(got).toEqual(want);
});

test.each([
  { name: 'active account', active: true, published: [['7', true]] },
  { name: 'disabled account', active: false, published: [] }
])('accepting the terms on an $name', async ({ active, published: want }) => {
  const event = actionEvent({ consent: 'yes' }) as { locals: Record<string, unknown> };
  event.locals.accountState = { value: { onboarded: false, active } };
  await actions.consent(event as never);
  expect({ saved, published }).toEqual({ saved: [['7', true]], published: [...want] });
});

test.each([
  { action: 'consent', consent: '', failed: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'consent', consent: 'yes', failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'consent', consent: 'yes', failed: false, status: undefined, counts: [] },
  { action: 'consent', consent: 'yes', failed: false, status: undefined, counts: [], session: { delegate_of: '2' } },
  { action: 'consent', consent: 'yes', failed: false, status: 403, counts: [], session: { impersonator_id: '3' } },
  { action: 'done', consent: '', failed: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'finishImport', consent: '', failed: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'done', consent: 'yes', failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'finishImport', consent: 'yes', failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'done', consent: 'yes', failed: false, status: 303, counts: [['onboarding.preset|quiet', 1]] },
  { action: 'finishImport', consent: 'yes', failed: false, status: undefined, counts: [['onboarding.preset|import', 1]] }
])('$action consent=$consent failed=$failed session=$session', async ({ action, consent, failed, status, counts, session }) => {
  saveFails = failed;
  const event = actionEvent({ consent, preset: 'quiet' });
  Object.assign(event.locals.session, session);
  const response = await Promise.resolve().then(() => actions[action](event as never)).catch((err) => err);
  expect({ status: response.status, counts: await recorded() }).toEqual({ status, counts: counts.map(([field, n]) => [field, n]) });
});
