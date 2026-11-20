// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
let saveFails = false;
let consentFails = false;
const writes: unknown[] = [];
const requestId = '11111111-2222-4333-8444-555555555555';
const acceptedForm = { consent: 'yes', terms_version: TERMS_VERSION, request_id: requestId };
const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  maskedContactEmail: async () => '',
  setTermsConsent: async (...args: unknown[]) => {
    if (consentFails) throw new Error('consent failed');
    writes.push(['consent', ...args]);
  },
  setOnboarded: async (...args: unknown[]) => {
    if (saveFails) throw new Error('save failed');
    writes.push(['onboarded', ...args]);
  },
  publishEventSub: async (...args: unknown[]) => {
    writes.push(['eventsub', ...args]);
  }
}));
mock.module('#lib/server/feature-presets', () => ({ applyFeaturePreset: async () => {} }));
const { actions, load } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  saveFails = false;
  consentFails = false;
  writes.length = 0;
});

test('counts the welcome view once', async () => {
  await load(actionEvent() as never);
  expect(await recorded()).toEqual([['onboarding.steps|viewed', 1]]);
});

test.each([
  { name: 'owner without state', session: {}, accountState: undefined, want: { landing: null, accepted: false } },
  { name: 'unaccepted owner', session: {}, accountState: { value: { onboarded: false } }, want: { landing: null, accepted: false } },
  { name: 'onboarded owner with legacy terms', session: {}, accountState: { value: { onboarded: true } }, want: { landing: null, accepted: false } },
  { name: 'accepted owner', session: {}, accountState: { value: { onboarded: true, termsVersion: TERMS_VERSION } }, want: { landing: null, accepted: true } },
  { name: 'unaccepted delegate', session: { delegate_of: '2' }, accountState: { value: { onboarded: false } }, want: { landing: '/', accepted: undefined } },
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
  { name: 'active account', active: true, marketing: 'yes', published: [['eventsub', '7', true]] },
  { name: 'disabled account', active: false, marketing: '', published: [] }
])('accepting the terms on an $name', async ({ active, marketing, published }) => {
  const event = actionEvent({ ...acceptedForm, marketing, locale: 'fr' }) as { locals: Record<string, unknown> };
  event.locals.accountState = { value: { onboarded: false, active } };
  const result = await actions.consent(event as never);
  expect(result).toEqual({ ok: true });
  expect(writes).toEqual([
    ['consent', '7', { source: 'welcome', marketing: marketing === 'yes', locale: 'fr', requestId }],
    ['onboarded', '7', true],
    ...published
  ]);
});

test.each([
  { action: 'consent', accepted: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'consent', accepted: true, consentFailed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'consent', accepted: true, failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'consent', accepted: true, status: undefined, counts: [] },
  { action: 'consent', accepted: true, status: 403, counts: [], session: { delegate_of: '2' } },
  { action: 'consent', accepted: true, status: 403, counts: [], session: { impersonator_id: '3' } },
  { action: 'done', accepted: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'finishImport', accepted: false, status: 400, counts: [['onboarding.steps|consent_missing', 1]] },
  { action: 'done', accepted: true, failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'finishImport', accepted: true, failed: true, status: 502, counts: [['onboarding.steps|save_failed', 1]] },
  { action: 'done', accepted: true, status: 303, counts: [['onboarding.preset|quiet', 1]] },
  { action: 'finishImport', accepted: true, status: undefined, counts: [['onboarding.preset|import', 1]] }
])('$action accepted=$accepted failed=$failed session=$session', async (row) => {
  saveFails = row.failed ?? false;
  consentFails = row.consentFailed ?? false;
  const event = actionEvent({ ...acceptedForm, consent: row.accepted ? 'yes' : '', preset: 'quiet' });
  Object.assign(event.locals, {
    session: { ...event.locals.session, ...row.session },
    accountState: { value: { termsVersion: row.accepted ? TERMS_VERSION : '' } }
  });
  const response = await Promise.resolve().then(() => actions[row.action](event as never)).catch((err) => err);
  expect({ status: response.status, counts: await recorded() }).toEqual({
    status: row.status, counts: row.counts.map(([field, n]) => [field, n])
  });
  if (row.status === 403 || row.consentFailed) expect(writes).toEqual([]);
});
