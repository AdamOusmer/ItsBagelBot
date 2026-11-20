// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { FREEZE_MAX_MINUTES } from '@bagel/kit/catalog';
import { stubSvelteKit } from '../../../../test/sveltekit';
import raffleWire from '../../../../../../internal/domain/rpc/draw/testdata/wire.json';

stubSvelteKit();
const nats = await import('@bagel/kit/server/nats');
const { RpcError } = nats;
const { SUB } = await import('#lib/server/services');
const user = raffleWire.request.user_id;
const stamp = { freeze_id: 'epoch', frozen_as_of: 100, expires_at: 200 };
const freezeStamp = { id: stamp.freeze_id, asOf: stamp.frozen_as_of, expiresAt: stamp.expires_at };
const locals = { session: { user_id: user }, locale: 'en' };
const moderator = { user_id: '95', delegate_of: user, sections: ['modules'] };
const calls: { subject: string; payload: unknown; timeoutMs: number }[] = [];
let replies: Record<string, object | Error>;
const loyaltyRow = { name: 'loyalty', is_enabled: true, configs: { pointsName: 'bagels' } };

const valkey = await import('@bagel/kit/server/valkey-store');
mock.module('@bagel/kit/server/valkey-store', () => ({
  ...valkey,
  getModules: async () => ({
    projected: true,
    modules: [loyaltyRow]
  })
}));

mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, payload: unknown, timeoutMs: number) => {
    calls.push({ subject, payload, timeoutMs });
    const reply = replies[subject] ?? replies[subject.slice(SUB.loyalty.length + 1)];
    if (reply instanceof Error) throw reply;
    return reply;
  }
}));

const { actions, load } = await import('./+page.server');
const { GET } = await import('./status/+server');
const { eligibleCount } = await import('#lib/server/loyalty-store');
const ok = { ok: true };
const invalid = { status: 400, data: { ok: false } };
const conflict = (reason: string) => ({ status: 409, data: { ok: false, reason } });
const times = { frozen_as_of: stamp.frozen_as_of, expires_at: stamp.expires_at };
const identity = { user_id: user, actor_id: moderator.user_id };
const set = { ...identity, minutes: FREEZE_MAX_MINUTES };

const cases = [
  { action: 'freeze', fields: { minutes: String(FREEZE_MAX_MINUTES) },
    reply: times, want: ok, verb: 'freeze.set', payload: set },
  { action: 'freeze', fields: { minutes: '1' },
    reply: times, want: ok, verb: 'freeze.set', payload: { ...set, minutes: 1 } },
  { action: 'freeze', fields: { minutes: String(FREEZE_MAX_MINUTES) },
    reply: new Error('RPC timed out'),
    want: { status: 502, data: { ok: false } }, verb: 'freeze.set', payload: set },
  ...['', '0', '1.5', String(FREEZE_MAX_MINUTES + 1)].map((minutes) => ({
    action: 'freeze', fields: { minutes }, reply: {}, want: invalid, verb: '', payload: null
  })),
  ...[true, false].map((released) => ({
    action: 'unfreeze', fields: {}, reply: { released }, want: ok, verb: 'freeze.release', payload: identity
  })),
  { action: 'reset', fields: { confirm: 'reset' }, reply: new RpcError('frozen', 'conflict'),
    want: conflict('frozen'), verb: 'balance.reset', payload: { user_id: user } }
];

test.each(cases)('$action with $fields answers $want', async ({ action, fields, reply: next, want, verb, payload }) => {
  replies = { [verb]: next };
  calls.length = 0;
  const request = new Request(`https://dashboard.test/loyalty?/${action}`, {
    method: 'POST',
    body: new URLSearchParams(fields as Record<string, string>)
  });
  const result = await actions[action]({ request, locals: { ...locals, session: moderator } } as never);
  const timeoutMs = verb === 'freeze.set' ? 30_000 : 4000;
  expect({ result, calls }).toMatchObject({
    result: want,
    calls: verb ? [{ subject: `${SUB.loyalty}.${verb}`, payload, timeoutMs }] : []
  });
});

test.each([
  { name: 'a failed freeze read', reply: new Error('freeze unavailable'), expected: null },
  { name: 'an inactive freeze', reply: { active: false }, expected: null },
  { name: 'an active freeze', reply: { active: true, ...times }, expected: { asOf: freezeStamp.asOf, expiresAt: freezeStamp.expiresAt } }
])('load handles $name independently of the top standings', async ({ reply, expected }) => {
  replies = { 'freeze.get': reply, 'top.get': { ...stamp } };
  const page = await load({ locals, cookies: { get: () => undefined } } as never);
  expect(page).toMatchObject({
    enabled: true,
    config: { pointsName: 'bagels' },
    freeze: expected,
    topFreeze: freezeStamp
  });
});

test.each([
  { name: 'owner', session: locals.session, userId: user, status: 200 },
  { name: 'delegate', session: { user_id: '8', delegate_of: user, sections: ['modules'] }, userId: user, status: 200 },
  { name: 'signed out', session: null, userId: null, status: 401 }
])('raffle status scopes the request for $name', async ({ session, userId, status }) => {
  const { subject, reply } = raffleWire;
  replies = { [subject]: reply };
  calls.length = 0;
  const res = await GET({ locals: { ...locals, session } } as never);
  expect({ code: res.status, cache: res.headers.get('cache-control'), view: await res.json(), calls }).toEqual({
    code: status,
    cache: 'no-store',
    view: userId ? reply : null,
    calls: userId ? [{ subject, payload: { ...raffleWire.request, user_id: userId }, timeoutMs: 4000 }] : []
  });
});

test.each([
  { name: 'gamble', conflict: false, want: { ok: true, conflict: false, rev: 5 } },
  { name: 'gamble', conflict: true, want: { ok: false, conflict: true, rev: 5 } },
  { name: 'raffle', conflict: false, want: invalid },
  { name: 'loyalty', conflict: false, want: invalid }
])('gameSettings patches only a loyalty child ($name, conflict: $conflict)', async ({ name, conflict, want }) => {
  const subject = `${SUB.modules}.patch`;
  const projector = `${SUB.projector}.modules.replace`;
  const modules = [loyaltyRow, { name: 'gamble', is_enabled: true, revision: 5, configs: { winPercent: '55' } }];
  replies = { [subject]: { rev: 5, conflict, modules }, [projector]: { modules } };
  calls.length = 0;
  const cookies = { set: mock() };
  const url = new URL('https://dashboard.test/loyalty?/gameSettings');
  const request = new Request(url, {
    method: 'POST',
    body: new URLSearchParams({
      name, is_enabled: 'on', expected_rev: '4',
      partial: JSON.stringify({ winPercent: '55', claimPrize: 'legacy' })
    })
  });
  const result = await actions.gameSettings({ request, locals, cookies, url } as never);
  expect({
    result,
    rpc: calls.find((call) => call.subject === subject)?.payload,
    refresh: cookies.set.mock.calls.length
  }).toEqual({
    result: want,
    rpc: name === 'gamble'
      ? { user_id: user, name, is_enabled: true, expected_rev: 4, configs: { winPercent: '55' } }
      : undefined,
    refresh: conflict ? 1 : 0
  });
});

const countVerb = 'balance.count_at_least';
const minimums = { minPoints: 25, minWatchSeconds: 7200 };

test.each([
  { name: 'zero on live standings', reply: { count: 0 }, expected: { count: 0, freeze: null }, error: null },
  { name: 'a frozen count', reply: { count: 3, ...stamp }, expected: { count: 3, freeze: freezeStamp }, error: null },
  { name: 'a reply without a count', reply: { ...stamp }, expected: null, error: 'missing loyalty standings count' }
])('eligibleCount handles $name and sends both minimums', async ({ reply, expected, error }) => {
  replies = { [countVerb]: reply };
  calls.length = 0;
  const count = eligibleCount(user, minimums);
  if (error) await expect(count).rejects.toThrow(error);
  else expect(await count).toEqual(expected);
  expect(calls).toEqual([{
    subject: `${SUB.loyalty}.${countVerb}`,
    payload: { user_id: user, min_points: 25, min_watch_seconds: 7200 },
    timeoutMs: 4000
  }]);
});
