// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let accountReply: () => Promise<unknown> = async () => ({});

const services = await import('./services');
const revocation = await import('@bagel/kit/server/session-revocation');
mock.module('$lib/server/services', () => ({
  ...services,
  accountState: () => accountReply(),
  delegationAccess: async () => [],
  isBanned: async () => false
}));
mock.module('@bagel/kit/server/session-revocation', () => ({ ...revocation, isSessionRevoked: async () => false }));

const { RpcError } = await import('@bagel/kit/server/nats');
const { guardSession } = await import('./guard');

type Outcome = { redirected: string | null; wiped: boolean; accountState: unknown };

async function run(): Promise<Outcome> {
  const locals: Record<string, unknown> = {};
  let wiped = false;
  const event = {
    url: new URL('https://dash.example/commands'),
    route: { id: '/(app)/commands' },
    locals,
    cookies: { delete: () => (wiped = true), set: () => {} }
  } as never;
  const session = { sid: 's1', user_id: '42', iat: 1, expires_at: 2 } as never;
  try {
    await guardSession(event, session);
    return { redirected: null, wiped, accountState: locals.accountState };
  } catch (e) {
    const refusal = e as { status?: number; location?: string };
    if (refusal.status !== 303) throw e;
    return { redirected: refusal.location ?? '', wiped, accountState: locals.accountState };
  }
}

const kept = { redirected: null, wiped: false, accountState: undefined };
const bounced = { redirected: '/login?e=signedout', wiped: true };

const cases: { name: string; reply: () => Promise<unknown>; want: Outcome }[] = [
  {
    name: 'a fulfilled read keeps the session and publishes the state',
    reply: async () => ({ plan: 'free' }),
    want: { ...kept, accountState: { value: { plan: 'free' } } }
  },
  {
    name: 'not_found clears the cookie and bounces to login',
    reply: async () => {
      throw new RpcError('no such user', 'not_found');
    },
    want: { ...bounced, accountState: { ghost: true } }
  },
  {
    name: 'an uncoded refusal keeps the session',
    reply: async () => {
      throw new RpcError('db stalled');
    },
    want: kept
  },
  ...(['internal', 'unavailable', 'invalid'] as const).map((code) => ({
    name: `${code} keeps the session and lets the layout retry`,
    reply: async () => {
      throw new RpcError('users service hiccup', code);
    },
    want: kept
  })),
  {
    name: 'a non-RpcError blip keeps the session',
    reply: async () => {
      throw new Error('socket closed');
    },
    want: kept
  }
];

describe('ghost-session gate', () => {
  test.each(cases)('$name', async ({ reply, want }) => {
    accountReply = reply;
    expect(await run()).toEqual(want);
  });
});
