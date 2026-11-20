// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { stubSvelteKit } from '../../../test/sveltekit';
import { actionEvent } from '../../../test/analytics';

stubSvelteKit();
const calls: Array<[subject: string, payload: unknown]> = [];
let reply: Record<string, unknown>;
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, payload: unknown) => {
    calls.push([subject, payload]);
    return reply;
  }
}));

const { actions, load } = await import('./+page.server');
const UUID = '11111111-2222-4333-8444-555555555555';
const reader = (next: string) => new URL(`https://dashboard.itsbagelbot.com/consent?${new URLSearchParams({ next })}`);
const acceptance = { consent: 'yes', terms_version: TERMS_VERSION, request_id: UUID, locale: 'fr' };

beforeEach(() => {
  calls.length = 0;
  reply = { ok: true };
});

test.each([
  { name: 'terms only', fields: {}, next: '/commands?tab=1', status: 303, location: '/commands?tab=1', write: false },
  { name: 'express opt-in', fields: { marketing: 'yes' }, status: 303, location: '/', write: true },
  { name: 'unavailable opt-in continues', fields: { marketing: 'yes' }, skip: 'unavailable', status: 303, location: '/', write: true },
  { name: 'missing email continues', fields: { marketing: 'yes' }, skip: 'no_email', status: 303, location: '/', write: true },
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
  { name: 'refused write', refused: true, status: 502, write: false }
])('consent action: $name', async (row) => {
  const event = actionEvent({ ...acceptance, ...row.fields } as Record<string, string>);
  const session = row.session === undefined ? event.locals.session : row.session;
  const locals = { ...event.locals, session };
  reply = { ok: !row.refused, marketing_skipped: row.skip };
  const result = await Promise.resolve().then(() => actions.default({ ...event, locals, url: reader(row.next ?? '/') } as never)).catch((err) => err);
  const payload = {
    broadcaster_user_id: '7', actor_id: '7', terms_version: TERMS_VERSION,
    marketing: row.write, source: 'consent', locale: 'fr', request_id: UUID
  };
  expect(result).toMatchObject(row.location ? { status: row.status, location: row.location } : { status: row.status });
  expect(calls).toEqual(row.write === undefined ? [] : [['rpc.dashboard.consent_set', payload]]);
});

test.each([
  { name: 'owner', session: { user_id: '7' }, terms: '', landing: null },
  { name: 'current owner', session: { user_id: '7' }, terms: TERMS_VERSION, landing: '/settings' },
  { name: 'delegate', session: { user_id: '7', delegate_of: '8' }, terms: '', landing: '/' },
  { name: 'impersonation', session: { user_id: '7', impersonator_id: '9' }, terms: '', landing: '/' }
])('consent load: $name', async ({ session, terms, landing }) => {
  reply = { masked_email: 'c***@example.test' };
  const locals = { session, locale: 'en', accountState: { value: { termsVersion: terms } } };
  const result = await Promise.resolve().then(() => load({ locals, url: reader('/settings') } as never)).catch((err) => err);
  if (landing) {
    expect({ location: result.location, calls }).toEqual({ location: landing, calls: [] });
    return;
  }
  expect(result).toEqual({ requestId: expect.stringMatching(/^[\da-f-]{36}$/), maskedEmail: reply.masked_email });
  expect(calls).toEqual([['rpc.dashboard.state_get', { broadcaster_user_id: '7', include_masked_email: true }]]);
});
