// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent } from '../../../test/analytics';

stubSvelteKit();
const calls: Array<[subject: string, payload: unknown]> = [];
const operations: string[] = [];
const stateGet = 'rpc.dashboard.state_get';
const consentSet = 'rpc.dashboard.consent_set';
const onboardedSet = 'rpc.dashboard.onboarded_set';
let reply: Record<string, unknown>;
let stateReply: Record<string, unknown> | undefined;
let failedSubject: string | undefined;

function recordOperation(subject: string): void {
  operations.push(subject);
  if (subject === failedSubject) throw new Error(`${subject} unavailable`);
}

const published = mock(async (subject: string, _payload: unknown, _options: unknown) => {
  recordOperation(subject);
});
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  publish: published,
  rpc: async (subject: string, payload: unknown) => {
    calls.push([subject, payload]);
    recordOperation(subject);
    if (subject === stateGet && stateReply) return stateReply;
    return reply;
  }
}));

const { SUB } = await import('#lib/server/services/subjects');
const { actions, load } = await import('./+page.server');
const UUID = '11111111-2222-4333-8444-555555555555';
const mailingAddress = 'test mailing address';
function reader(next: string, skipped?: string): URL {
  const query = new URLSearchParams({ next });
  if (skipped) query.set('skipped', skipped);
  return new URL(`https://dashboard.itsbagelbot.com/consent?${query}`);
}
const acceptance = { consent: 'yes', terms_version: TERMS_VERSION, request_id: UUID, locale: 'fr' };

beforeEach(() => {
  calls.length = 0;
  operations.length = 0;
  published.mockClear();
  reply = { ok: true };
  stateReply = undefined;
  failedSubject = undefined;
  privateEnv.BUSINESS_MAILING_ADDRESS = mailingAddress;
});

type ActionCase = {
  name: string;
  fields?: Record<string, string>;
  next?: string;
  status?: number;
  location?: string;
  write?: boolean;
  skip?: 'unavailable' | 'no_email';
  session?: Record<string, string> | null;
  refused?: boolean;
  address?: string;
  subscribed?: boolean;
  onboarded?: boolean;
  active?: boolean;
};

const actionCases: ActionCase[] = [
  { name: 'terms only', fields: {}, next: '/commands?tab=1', status: 303, location: '/commands?tab=1', write: false },
  { name: 'express opt-in', fields: { marketing: 'yes' }, status: 303, location: '/', write: true },
  { name: 'unset address suppresses a posted opt-in', fields: { marketing: 'yes' }, address: '', status: 303, location: '/', write: false },
  { name: 'existing subscription is preserved', fields: { marketing: 'yes' }, subscribed: true, status: 303, location: '/', write: false },
  { name: 'unavailable opt-in shows the notice', fields: { marketing: 'yes' }, skip: 'unavailable', next: '/settings', status: 303, location: '/consent?skipped=unavailable&next=%2Fsettings', write: true },
  { name: 'missing email preserves query and fragment', fields: { marketing: 'yes' }, skip: 'no_email', next: '/commands?tab=1#editor', status: 303, location: '/consent?skipped=no_email&next=%2Fcommands%3Ftab%3D1%23editor', write: true },
  { name: 'active owner finishes onboarding', onboarded: false, active: true, status: 303, location: '/', write: false },
  { name: 'inactive owner finishes onboarding', onboarded: false, active: false, status: 303, location: '/', write: false },
  { name: 'untrusted source and actor', fields: { source: 'welcome', actor_id: '8' }, status: 303, location: '/', write: false },
  { name: 'unchecked terms', fields: { consent: '', marketing: 'yes' }, status: 400 },
  { name: 'missing request ID', fields: { request_id: '' }, status: 400 },
  { name: 'stale displayed version', fields: { terms_version: '2025-01' }, status: 400 },
  { name: 'signed out', session: null, status: 401 },
  { name: 'delegate', session: { user_id: '7', delegate_of: '8' }, status: 403 },
  { name: 'impersonation', session: { user_id: '7', impersonator_id: '9' }, status: 403 },
  { name: 'off-site return', next: '//example.test/settings', status: 303, location: '/', write: false },
  { name: 'backslash return', next: '/\\example.test', status: 303, location: '/', write: false },
  { name: 'self return', next: '/consent/?next=/settings', status: 303, location: '/', write: false },
  { name: 'refused terms write does not onboard', refused: true, onboarded: false, active: true, status: 502, write: false }
];

function onboardingCalls(row: ActionCase): { writes: [string, unknown][]; events: Parameters<typeof published>[] } {
  const onboarding = row.onboarded === false && !row.refused;
  return {
    writes: onboarding ? [[onboardedSet, { broadcaster_user_id: '7', onboarded: true }]] : [],
    events: onboarding && row.active ? [[SUB.outgress, { type: 'eventsub', broadcaster_id: '7', payload: { enabled: true } }, { flush: true }]] : []
  };
}

async function submitConsent(event: Parameters<typeof actions.default>[0]) {
  return Promise.resolve().then(() => actions.default(event)).catch((err) => err);
}

function requestLocals(row: ActionCase, session: ReturnType<typeof actionEvent>['locals']['session']) {
  const value = { onboarded: row.onboarded ?? true, active: row.active, marketingStatus: row.subscribed ? 'granted' : 'none' };
  return { session: row.session === undefined ? session : row.session, accountState: { value } };
}

test.each(actionCases)('consent action: $name', async (row) => {
  const event = actionEvent({ ...acceptance, ...row.fields });
  const locals = { ...event.locals, ...requestLocals(row, event.locals.session) };
  if (row.address !== undefined) privateEnv.BUSINESS_MAILING_ADDRESS = row.address;
  reply = { ok: !row.refused, marketing_skipped: row.skip };
  const url = reader(row.next ?? '/');
  const result = await submitConsent({ ...event, locals, url } as never);
  const payload = {
    broadcaster_user_id: '7', actor_id: '7', terms_version: TERMS_VERSION,
    marketing: row.write, source: 'consent', locale: 'fr', request_id: UUID
  };
  const response = row.location ? { status: row.status, location: row.location } : { status: row.status };
  const { writes, events } = onboardingCalls(row);
  expect(result).toMatchObject(response);
  expect({ calls, published: published.mock.calls }).toEqual({
    calls: row.write === undefined ? [] : [[consentSet, payload], ...writes], published: events
  });
});

test.each([
  { name: 'active new owner', status: 303, want: [stateGet, consentSet, SUB.outgress, onboardedSet] },
  { name: 'already onboarded owner', onboarded: true, status: 303, want: [stateGet, consentSet] },
  { name: 'state read still fails', failure: stateGet, status: 502, want: [stateGet] },
  { name: 'EventSub cannot be published', failure: SUB.outgress, status: 502, want: [stateGet, consentSet, SUB.outgress] },
  { name: 'onboarding write fails', failure: onboardedSet, status: 502, want: [stateGet, consentSet, SUB.outgress, onboardedSet] }
])('consent submission with unreadable request state: $name', async ({ onboarded, failure, status, want }) => {
  stateReply = { active: true, onboarded: onboarded ?? false };
  failedSubject = failure;
  const result = await submitConsent({ ...actionEvent(acceptance), url: reader('/') } as never);
  expect({ status: result.status, operations }).toEqual({ status, operations: [...want] });
});

test.each([
  { name: 'owner with a mailing address', session: { user_id: '7' }, terms: '', landing: null, address: mailingAddress, masked: true },
  { name: 'owner without a mailing address', session: { user_id: '7' }, terms: '', landing: null, address: '' },
  { name: 'skip parameter before acceptance', session: { user_id: '7' }, terms: '', landing: null, address: '', skipped: 'no_email' },
  { name: 'subscribed owner', session: { user_id: '7' }, terms: '', landing: null, address: mailingAddress, subscribed: true },
  { name: 'current owner', session: { user_id: '7' }, terms: TERMS_VERSION, landing: '/settings' },
  { name: 'unknown skip reason', session: { user_id: '7' }, terms: TERMS_VERSION, landing: '/settings', skipped: 'unknown' },
  { name: 'delegate', session: { user_id: '7', delegate_of: '8' }, terms: TERMS_VERSION, landing: '/', skipped: 'no_email' },
  { name: 'impersonation', session: { user_id: '7', impersonator_id: '9' }, terms: '', landing: '/' }
])('consent load: $name', async ({ session, terms, landing, address, subscribed, masked, skipped }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address || undefined;
  reply = { masked_email: 'c***@example.test' };
  const value = { termsVersion: terms, marketingStatus: subscribed ? 'granted' : 'none' };
  const locals = { session, locale: 'en', accountState: { value } };
  const url = reader('/settings', skipped);
  const result = await Promise.resolve().then(() => load({ locals, url } as never)).catch((err) => err);
  if (landing) {
    expect({ location: result.location, calls }).toEqual({ location: landing, calls: [] });
    return;
  }
  expect(result).toEqual({
    consent: {
      requestId: expect.stringMatching(/^[\da-f-]{36}$/), mailingAddress: address,
      subscribed: subscribed ?? false, maskedEmail: masked ? reply.masked_email : ''
    },
    notice: null
  });
  const expectedCalls: typeof calls = [];
  if (skipped) expectedCalls.push([stateGet, { broadcaster_user_id: '7' }]);
  if (masked) expectedCalls.push([stateGet, { broadcaster_user_id: '7', include_masked_email: true }]);
  expect(calls).toEqual(expectedCalls);
});

test.each([
  { skipped: 'no_email', next: '/commands?tab=1#editor', destination: '/commands?tab=1#editor' },
  { skipped: 'unavailable', next: '/settings', destination: '/settings' },
  { skipped: 'no_email', next: '/settings', destination: '/settings', stale: true },
  { skipped: 'no_email', next: '//example.test', destination: '/' },
  { skipped: 'unavailable', next: '/consent/?next=/settings', destination: '/' }
])('consent notice for $skipped with return path $next (stale=$stale)', async ({ skipped, next, destination, stale }) => {
  const termsVersion = stale ? '' : TERMS_VERSION;
  const locals = { ...actionEvent().locals, accountState: { value: { termsVersion } } };
  reply = { terms_version: TERMS_VERSION };
  const result = await load({ locals, url: reader(next, skipped) } as never);
  expect(result).toEqual({ consent: null, notice: { skipped, next: destination } });
  expect(calls).toEqual(stale ? [[stateGet, { broadcaster_user_id: '7' }]] : []);
});
