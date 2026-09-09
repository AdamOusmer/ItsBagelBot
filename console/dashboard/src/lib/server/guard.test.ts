// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The ghost-session gate: which users-service refusals clear the cookie.
// Before the RPC code vocabulary every RpcError cleared it, so one transient
// `internal` refusal signed a live visitor out mid-session. Only an
// authoritative "no such user" may do that; an uncoded refusal keeps the
// pre-code behaviour because the users dashboard handlers still answer
// uncoded today (see GONE_CODES in guard.ts).
import { beforeEach, describe, expect, mock, test } from 'bun:test';

let accountReply: () => Promise<unknown> = async () => ({});

mock.module('$app/environment', () => ({ dev: false }));
mock.module('newrelic', () => ({
  default: { startSegment: (_n: string, _r: boolean, f: () => unknown) => f(), recordMetric: () => {} }
}));
mock.module('$lib/server/session', () => ({ COOKIE: 'bagel_session', seal: () => 'sealed' }));
mock.module('$lib/server/services', () => ({
  accountState: () => accountReply(),
  delegationAccess: async () => [],
  isBanned: async () => false
}));
mock.module('$lib/server/module-gate', () => ({ assertBetaRouteOpen: async () => {} }));
mock.module('@bagel/shared', () => ({ delegateAllowedPaths: () => ['/'], pathnameAllowed: () => true }));
mock.module('@bagel/shared/server/session-revocation', () => ({ isSessionRevoked: async () => false }));

const { RpcError } = await import('@bagel/shared/server/nats');
const { guardSession } = await import('./guard');

type Outcome = { redirected: string | null; wiped: boolean; locals: Record<string, unknown> };

async function run(): Promise<Outcome> {
  const out: Outcome = { redirected: null, wiped: false, locals: {} };
  const event = {
    url: new URL('https://dash.example/timers'),
    locals: out.locals,
    cookies: { delete: () => { out.wiped = true; }, set: () => {} }
  } as never;
  const session = { sid: 's1', user_id: '42', iat: 1, expires_at: 2 } as never;
  try {
    await guardSession(event, session);
  } catch (e) {
    const r = e as { status?: number; location?: string };
    if (r.status !== 303) throw e;
    out.redirected = r.location ?? '';
  }
  return out;
}

describe('ghost-session gate', () => {
  beforeEach(() => {
    accountReply = async () => ({ plan: 'free' });
  });

  test('a fulfilled read keeps the session and publishes the state', async () => {
    const out = await run();
    expect([out.redirected, out.wiped, out.locals.accountState]).toEqual([null, false, { value: { plan: 'free' } }]);
  });

  test('not_found clears the cookie and bounces to login', async () => {
    accountReply = async () => { throw new RpcError('no such user', 'not_found'); };
    const out = await run();
    expect([out.redirected, out.wiped, out.locals.accountState]).toEqual(['/login?e=signedout', true, { ghost: true }]);
  });

  test('an uncoded refusal still clears, matching the pre-code behaviour', async () => {
    accountReply = async () => { throw new RpcError('no such user'); };
    const out = await run();
    expect([out.redirected, out.wiped]).toEqual(['/login?e=signedout', true]);
  });

  test.each(['internal', 'unavailable', 'invalid'] as const)('%s keeps the session and lets the layout retry', async (code) => {
    accountReply = async () => { throw new RpcError('users service hiccup', code); };
    const out = await run();
    expect([out.redirected, out.wiped, 'accountState' in out.locals]).toEqual([null, false, false]);
  });

  test('a non-RpcError blip keeps the session', async () => {
    accountReply = async () => { throw new Error('socket closed'); };
    const out = await run();
    expect([out.redirected, out.wiped, 'accountState' in out.locals]).toEqual([null, false, false]);
  });
});
