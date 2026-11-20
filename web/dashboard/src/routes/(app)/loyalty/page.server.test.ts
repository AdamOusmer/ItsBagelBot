// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { FREEZE_MAX_MINUTES } from '@bagel/kit/catalog';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
const nats = await import('@bagel/kit/server/nats');
const { RpcError } = nats;
const { SUB } = await import('#lib/server/services');
const user = '7';
const stamp = { freeze_id: 'epoch', frozen_as_of: 100, expires_at: 200 };
const freezeStamp = { id: stamp.freeze_id, asOf: stamp.frozen_as_of, expiresAt: stamp.expires_at };
const locals = { session: { user_id: user }, locale: 'en' };
const calls: { subject: string; payload: unknown; timeoutMs: number }[] = [];
let replies: Record<string, object | Error>;

const valkey = await import('@bagel/kit/server/valkey-store');
mock.module('@bagel/kit/server/valkey-store', () => ({
  ...valkey,
  getModules: async () => ({
    projected: true,
    modules: [{ name: 'loyalty', is_enabled: true, configs: { pointsName: 'bagels' } }]
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
const ok = { ok: true };
const invalid = { status: 400, data: { ok: false } };
const conflict = (reason: string) => ({ status: 409, data: { ok: false, reason } });
const set = { user_id: user, holder: 'mod', minutes: FREEZE_MAX_MINUTES, block_transfer: true, close_games: false };
const release = { user_id: user, holder: 'mod', freeze_id: stamp.freeze_id };

const cases = [
  { action: 'freeze', fields: { minutes: String(FREEZE_MAX_MINUTES), block_transfer: 'on' },
    reply: { freeze: stamp }, want: ok, verb: 'freeze.set', payload: set },
  { action: 'freeze', fields: { minutes: '1', close_games: 'on' },
    reply: { freeze: stamp }, want: ok, verb: 'freeze.set', payload: { ...set, minutes: 1, block_transfer: false, close_games: true } },
  { action: 'freeze', fields: { minutes: String(FREEZE_MAX_MINUTES), block_transfer: 'on' },
    reply: new Error('RPC timed out'),
    want: { status: 502, data: { ok: false } }, verb: 'freeze.set', payload: set },
  ...['', '0', '1.5', String(FREEZE_MAX_MINUTES + 1)].map((minutes) => ({
    action: 'freeze', fields: { minutes }, reply: {}, want: invalid, verb: '', payload: null
  })),
  { action: 'unfreeze', fields: { freeze_id: stamp.freeze_id }, reply: { found: true },
    want: ok, verb: 'freeze.release', payload: release },
  { action: 'unfreeze', fields: { freeze_id: stamp.freeze_id }, reply: {},
    want: conflict('stale_pin'), verb: 'freeze.release', payload: release },
  { action: 'unfreeze', fields: { freeze_id: stamp.freeze_id }, reply: new RpcError('held_by_round', 'conflict'),
    want: conflict('held_by_round'), verb: 'freeze.release', payload: release },
  { action: 'unfreeze', fields: { freeze_id: '' }, reply: {}, want: invalid, verb: '', payload: null },
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
  const result = await actions[action]({ request, locals } as never);
  const timeoutMs = verb === 'freeze.set' ? 30_000 : 4000;
  expect({ result, calls }).toMatchObject({
    result: want,
    calls: verb ? [{ subject: `${SUB.loyalty}.${verb}`, payload, timeoutMs }] : []
  });
});

test.each([
  { name: 'a failed freeze read', reply: new Error('freeze unavailable'), expected: null },
  { name: 'an empty freeze view', reply: { freeze: { holders: [] } }, expected: null },
  {
    name: 'an active freeze',
    reply: { freeze: { ...stamp, holders: ['round:one'], block_transfer: true } },
    expected: { ...freezeStamp, holders: ['round:one'], blockTransfer: true, closeGames: false }
  }
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
  const subject = 'rpc.draw.raffle.status';
  const view = { accrual_health: 'healthy', unpaid: ['unpaid'] };
  replies = { [subject]: view };
  calls.length = 0;
  const res = await GET({ locals: { ...locals, session } } as never);
  expect({ code: res.status, view: await res.json(), calls }).toEqual({
    code: status,
    view: userId ? view : null,
    calls: userId ? [{ subject, payload: { user_id: userId, source: 'dashboard' }, timeoutMs: 4000 }] : []
  });
});

test('gameSettings sends the child revision and only accepts its current settings', async () => {
  const subject = `${SUB.modules}.patch`;
  const projector = `${SUB.projector}.modules.replace`;
  const row = { name: 'raffle', is_enabled: true, revision: 5, configs: { prizePoints: '3000' } };
  replies = { [subject]: { rev: 5, modules: [row] }, [projector]: { modules: [row] } };
  calls.length = 0;
  const url = new URL('https://dashboard.test/loyalty?/gameSettings');
  const request = new Request(url, {
    method: 'POST',
    body: new URLSearchParams({
      name: 'raffle', is_enabled: 'on', expected_rev: '4',
      partial: JSON.stringify({ prizePoints: '3000', claimPrize: 'legacy' })
    })
  });
  const result = await actions.gameSettings({ request, locals, url } as never);
  expect({ result, rpc: calls.find((call) => call.subject === subject)?.payload }).toEqual({
    result: { ok: true, conflict: false, rev: 5 },
    rpc: { user_id: user, name: 'raffle', is_enabled: true, expected_rev: 4, configs: { prizePoints: '3000' } }
  });
});
