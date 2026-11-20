// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent, captureAnalytics } from '../../../test/analytics';

stubSvelteKit();
let saveFails = false;
let consentFails = false;
let marketingSkipped: 'no_email' | undefined;
let publishFails = false;
const writes: unknown[] = [];
const mailingAddress = 'test mailing address';
const requestId = '11111111-2222-4333-8444-555555555555';
const acceptedForm = { consent: 'yes', terms_version: TERMS_VERSION, request_id: requestId };
const services = await import('../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  maskedContactEmail: async () => '',
  setTermsConsent: async (...args: unknown[]) => {
    if (consentFails) throw new Error('consent failed');
    writes.push(['consent', ...args]);
    return { ok: true, marketing_skipped: marketingSkipped };
  },
  setOnboarded: async (...args: unknown[]) => {
    if (saveFails) throw new Error('save failed');
    writes.push(['onboarded', ...args]);
  },
  publishEventSub: async (...args: unknown[]) => {
    writes.push(['eventsub', ...args]);
    if (publishFails) throw new Error('publish failed');
  }
}));
mock.module('#lib/server/feature-presets', () => ({ applyFeaturePreset: async () => {} }));
const { actions, load } = await import('./+page.server');
const recorded = captureAnalytics();
beforeEach(() => {
  saveFails = false;
  consentFails = false;
  marketingSkipped = undefined;
  publishFails = false;
  privateEnv.BUSINESS_MAILING_ADDRESS = mailingAddress;
  writes.length = 0;
});

test('counts the welcome view once', async () => {
  await load(actionEvent() as never);
  expect(await recorded()).toEqual([['onboarding.steps|viewed', 1]]);
});

test.each([
  { name: 'owner with unreadable state still gets a consent form', session: {}, accountState: undefined, address: mailingAddress, want: { landing: null, consentAddress: mailingAddress } },
  { name: 'unaccepted owner without a mailing address still gets a terms form', session: {}, accountState: { value: { onboarded: false } }, want: { landing: null, consentAddress: '' } },
  { name: 'onboarded owner with legacy terms', session: {}, accountState: { value: { onboarded: true } }, address: mailingAddress, want: { landing: null, consentAddress: mailingAddress } },
  { name: 'accepted owner', session: {}, accountState: { value: { onboarded: true, termsVersion: TERMS_VERSION } }, want: { landing: null, consentAddress: null } },
  { name: 'unaccepted delegate', session: { delegate_of: '2' }, accountState: { value: { onboarded: false } }, want: { landing: '/', consentAddress: null } },
  { name: 'accepted delegate', session: { delegate_of: '2' }, accountState: { value: { onboarded: true } }, want: { landing: '/', consentAddress: null } },
  { name: 'impersonation', session: { impersonator_id: '3' }, accountState: { value: { onboarded: false } }, want: { landing: '/', consentAddress: null } }
])('load for $name', async ({ session, accountState, address, want }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address;
  const event = actionEvent() as { locals: Record<string, unknown> & { session: Record<string, unknown> } };
  Object.assign(event.locals.session, session);
  event.locals.accountState = accountState;
  const got = await Promise.resolve(load(event as never)).then(
    (data) => ({ landing: null, consentAddress: (data as { consent: { mailingAddress: string } | null }).consent?.mailingAddress ?? null }),
    (e: { location?: string }) => ({ landing: e.location ?? null, consentAddress: null })
  );
  expect(got).toEqual(want);
});

test.each([
  { name: 'active account with an address', active: true, address: mailingAddress, marketing: true, published: [['eventsub', '7', true, { flush: true }]] },
  { name: 'disabled account without an address', active: false, marketing: false, published: [] },
  { name: 'marketing opt-in skipped by Users', active: false, address: mailingAddress, marketing: true, skipped: 'no_email' as const, published: [] }
])('accepting the terms on welcome: $name', async ({ active, address, marketing, skipped, published }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address;
  marketingSkipped = skipped;
  const event = actionEvent({ ...acceptedForm, marketing: 'yes', locale: 'fr' });
  const locals = { ...event.locals, accountState: { value: { onboarded: false, active } } };
  const result = await actions.consent({ ...event, locals } as never);
  expect(result).toEqual({ ok: true, marketingSkipped: skipped });
  expect(writes).toEqual([
    ['consent', locals.session, { source: 'welcome', marketing, locale: 'fr', requestId }],
    ...published,
    ['onboarded', '7', true]
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

test.each([
  { action: 'done', fails: false, status: 303, writes: [['eventsub', '7', true, { flush: true }], ['onboarded', '7', true]] },
  { action: 'finishImport', fails: true, status: 502, writes: [['eventsub', '7', true, { flush: true }]] }
])('welcome $action resumes active onboarding with saved terms', async (row) => {
  publishFails = row.fails;
  const event = actionEvent({ preset: 'quiet' });
  const locals = { ...event.locals, accountState: { value: { active: true, onboarded: false, termsVersion: TERMS_VERSION } } };
  const response = await Promise.resolve().then(() => actions[row.action]({ ...event, locals } as never)).catch((err) => err);
  expect({ status: response.status, writes }).toEqual({ status: row.status, writes: [...row.writes] });
});
