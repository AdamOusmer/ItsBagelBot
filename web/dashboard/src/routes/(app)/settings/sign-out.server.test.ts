// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

type Session = { user_id: string; sid?: string; expires_at: number; impersonator_id?: string };

const calls: string[] = [];
let deviceRevokeFails = false;

const services = await import('../../../lib/server/services');
const servicesMock = () => ({
  ...services,
  deviceGrantRevokeAll: async () => {
    calls.push('devices');
    if (deviceRevokeFails) throw new Error('users unavailable');
  },
  auditDashboardImpersonation: (session: Session, action: string) => {
    if (session.impersonator_id) calls.push(`audit:${action}`);
  }
});
mock.module('#lib/server/services', servicesMock);
mock.module('../../../lib/server/services', servicesMock);

const revocation = await import('@bagel/kit/server/session-revocation');
mock.module('@bagel/kit/server/session-revocation', () => ({
  ...revocation,
  revokeAllForUser: async () => {
    calls.push('sessions');
  },
  revokeSession: async () => {
    calls.push('this-session');
  }
}));

const { actions } = await import('./+page.server');

function signOut(session: Session) {
  const deleted: string[] = [];
  const event = {
    locals: { session, locale: 'en' },
    cookies: { delete: (name: string) => deleted.push(name) },
    url: new URL('https://dashboard.itsbagelbot.com/settings')
  };
  const outcome = Promise.resolve(actions.signOutEverywhere(event as never)).catch((thrown: unknown) => thrown);
  return { outcome, deleted };
}

describe('signOutEverywhere', () => {
  beforeEach(() => {
    calls.length = 0;
    deviceRevokeFails = false;
  });

  test('a failed device revoke keeps the user signed in and reports the failure', async () => {
    deviceRevokeFails = true;
    const { outcome, deleted } = signOut({ user_id: '1', sid: 's', expires_at: 9_999_999_999 });

    expect(await outcome).toMatchObject({ status: 502, data: { error: expect.any(String) } });
    expect(deleted).toEqual([]);
    expect(calls).toEqual(['devices']);
  });

  test('success revokes devices and sessions, then signs out', async () => {
    const { outcome, deleted } = signOut({ user_id: '1', sid: 's', expires_at: 9_999_999_999 });

    expect(await outcome).toMatchObject({ status: 303, location: '/login?e=revoked' });
    expect(deleted).toHaveLength(1);
    expect(calls).toEqual(['devices', 'sessions', 'this-session']);
  });

  test('an impersonating staff session is audited', async () => {
    const { outcome } = signOut({ user_id: '1', sid: 's', expires_at: 9_999_999_999, impersonator_id: '9' });

    await outcome;
    expect(calls).toContain('audit:sign_out_everywhere');
  });
});
