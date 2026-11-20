// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent } from '../../../test/analytics';

stubSvelteKit();
const calls: Array<[subject: string, payload: unknown]> = [];
let reply: Record<string, unknown>;
const published = mock(async () => {});
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  publish: published,
  rpc: async (subject: string, payload: unknown) => {
    calls.push([subject, payload]);
    return reply;
  }
}));

const { SUB } = await import('#lib/server/services/subjects');
const { actions, load } = await import('./+page.server');
const UUID = '11111111-2222-4333-8444-555555555555';
const mailingAddress = 'test mailing address';
const reader = (next: string) => new URL(`https://dashboard.itsbagelbot.com/consent?${new URLSearchParams({ next })}`);
const acceptance = { consent: 'yes', terms_version: TERMS_VERSION, request_id: UUID, locale: 'fr' };

beforeEach(() => {
  calls.length = 0;
  published.mockClear();
  reply = { ok: true };
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
  { name: 'unavailable opt-in continues', fields: { marketing: 'yes' }, skip: 'unavailable', next: '/settings', write: true },
  { name: 'missing email continues', fields: { marketing: 'yes' }, skip: 'no_email', next: '/commands?tab=1', write: true },
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

function onboardingCalls(row: ActionCase): { writes: [string, unknown][]; events: unknown[][] } {
  const onboarding = row.onboarded === false && !row.refused;
  return {
    writes: onboarding ? [['rpc.dashboard.onboarded_set', { broadcaster_user_id: '7', onboarded: true }]] : [],
    events: onboarding && row.active ? [[SUB.outgress, { type: 'eventsub', broadcaster_id: '7', payload: { enabled: true } }, { flush: false }]] : []
  };
}

test.each(actionCases)('consent action: $name', async (row) => {
  const event = actionEvent({ ...acceptance, ...row.fields });
  const session = row.session === undefined ? event.locals.session : row.session;
  const value = { onboarded: row.onboarded ?? true, active: row.active, marketingStatus: row.subscribed ? 'granted' : 'none' };
  const locals = { ...event.locals, session, accountState: { value } };
  if (row.address !== undefined) privateEnv.BUSINESS_MAILING_ADDRESS = row.address;
  reply = { ok: !row.refused, marketing_skipped: row.skip };
  const result = await Promise.resolve().then(() => actions.default({ ...event, locals, url: reader(row.next ?? '/') } as never)).catch((err) => err);
  const payload = {
    broadcaster_user_id: '7', actor_id: '7', terms_version: TERMS_VERSION,
    marketing: row.write, source: 'consent', locale: 'fr', request_id: UUID
  };
  const response = row.location ? { status: row.status, location: row.location } : { status: row.status };
  const { writes, events } = onboardingCalls(row);
  expect(result).toMatchObject(row.skip ? { next: row.next, marketingSkipped: row.skip } : response);
  expect({ calls, published: published.mock.calls as unknown[][] }).toEqual({
    calls: row.write === undefined ? [] : [['rpc.dashboard.consent_set', payload], ...writes], published: events
  });
});

test.each([
  { name: 'owner with a mailing address', session: { user_id: '7' }, terms: '', landing: null, address: mailingAddress, masked: true },
  { name: 'owner without a mailing address', session: { user_id: '7' }, terms: '', landing: null, address: '' },
  { name: 'subscribed owner', session: { user_id: '7' }, terms: '', landing: null, address: mailingAddress, subscribed: true },
  { name: 'current owner', session: { user_id: '7' }, terms: TERMS_VERSION, landing: '/settings' },
  { name: 'delegate', session: { user_id: '7', delegate_of: '8' }, terms: '', landing: '/' },
  { name: 'impersonation', session: { user_id: '7', impersonator_id: '9' }, terms: '', landing: '/' }
])('consent load: $name', async ({ session, terms, landing, address, subscribed, masked }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address || undefined;
  reply = { masked_email: 'c***@example.test' };
  const value = { termsVersion: terms, marketingStatus: subscribed ? 'granted' : 'none' };
  const locals = { session, locale: 'en', accountState: { value } };
  const result = await Promise.resolve().then(() => load({ locals, url: reader('/settings') } as never)).catch((err) => err);
  if (landing) {
    expect({ location: result.location, calls }).toEqual({ location: landing, calls: [] });
    return;
  }
  expect(result).toEqual({
    requestId: expect.stringMatching(/^[\da-f-]{36}$/), mailingAddress: address,
    subscribed: subscribed ?? false, maskedEmail: masked ? reply.masked_email : ''
  });
  expect(calls).toEqual(masked ? [['rpc.dashboard.state_get', { broadcaster_user_id: '7', include_masked_email: true }]] : []);
});
