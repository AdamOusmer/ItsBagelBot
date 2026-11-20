// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { SUB } from './services/subjects';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const bus = await import('@bagel/kit/server/nats');
const moduleBlob = await import('./module-blob');
const store = await import('./channel-giveaways');

const uid = '42';
const round = '32ff6551-4646-4fb4-bf55-02268d943b1a';
const actor = { userId: uid, actorId: '95' };
const winner = { round_id: round, slot: 1, viewer_id: '81', login: 'viewer', prize: 'A game', won_at: 1, claimed: true, delivered_at: 2 };

afterEach(() => mock.restore());

test.each([
  { verb: 'live', input: {}, response: { rounds: [] }, run: () => store.liveGiveaways(uid), expected: { rounds: [], degraded: false } },
  {
    verb: 'eligible_count', input: { preset: 'regulars' }, response: { count: 0, frozen_as_of: 10, expires_at: 99 },
    run: () => store.eligibleCount(uid, 'regulars'), expected: { count: 0 }
  }
])('reads draw $verb with dashboard identity', async ({ verb, input, response, run, expected }) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce(response);
  expect(await run()).toEqual(expected);
  expect(rpc).toHaveBeenCalledWith(`rpc.draw.giveaway.${verb}`, { user_id: uid, source: 'dashboard', ...input }, 10_000);
});

test.each([
  { verb: 'start', input: { winners: 4, window_seconds: 180, preset: 'regulars', prize: 'A game' } },
  { verb: 'close', input: { round_id: round } },
  { verb: 'extend', input: { round_id: round } },
  { verb: 'cancel', input: { round_id: round } },
  { verb: 'reroll', input: { round_id: round, slot: 0 } },
  { verb: 'end_slots', input: { round_id: round, slot: 3 } },
  { verb: 'skip_rule', input: { round_id: round, slot: 1 } },
  { verb: 'mark_delivered', input: { round_id: round, slot: 1, delivered: false } }
] as const)('sends $verb with channel and actor IDs', async ({ verb, input }) => {
  const rpc = spyOn(bus, 'rpcReply').mockResolvedValueOnce({ done: true });
  expect(await store.mutateGiveaway(verb, actor, input)).toMatchObject({ done: true });
  expect(rpc).toHaveBeenCalledWith(`rpc.draw.giveaway.${verb}`, { user_id: uid, source: 'dashboard', actor_id: actor.actorId, ...input }, 10_000);
});

test('keeps the start refusal reason and code', async () => {
  spyOn(bus, 'rpcReply').mockResolvedValueOnce({ code: 'forbidden', error: 'refused', reason: 'giveaway.prize_blocked' });
  await expect(store.mutateGiveaway('start', actor, {})).rejects.toMatchObject({
    reason: 'giveaway.prize_blocked', refusal: { code: 'forbidden' }
  });
});

test('reports a degraded live read after a timeout', async () => {
  spyOn(bus, 'rpc').mockRejectedValueOnce(new Error('timeout'));
  expect(await store.liveGiveaways(uid)).toEqual({ rounds: [], degraded: true });
});

test.each<{
  enabled: boolean;
  reply: object;
  result: Awaited<ReturnType<typeof store.publicGiveawayWinners>>;
  calls: number;
}>([
  { enabled: false, reply: {}, result: { winners: [], degraded: false }, calls: 0 },
  { enabled: true, reply: new Error('timeout'), result: { winners: [], degraded: true }, calls: 1 },
  {
    enabled: true, reply: { winners: [winner, { ...winner, slot: 2, viewer_id: '82', login: '' }] }, calls: 1,
    result: {
      winners: [
        { round_id: round, slot: 1, login: 'viewer', prize: 'A game', won_at: 1 },
        { round_id: round, slot: 2, login: '', prize: 'A game', won_at: 1 }
      ],
      degraded: false
    }
  }
])('projects public winners for enabled=$enabled and $calls history reads', async ({ enabled, reply, result, calls }) => {
  spyOn(moduleBlob, 'readModuleBlob').mockResolvedValueOnce({ enabled, configs: {} });
  const rpc = spyOn(bus, 'rpc').mockImplementationOnce(async () => {
    if (reply instanceof Error) throw reply;
    return reply as never;
  });
  expect(await store.publicGiveawayWinners(uid)).toEqual(result);
  expect(rpc.mock.calls).toEqual(calls ? [[`${SUB.modules}.giveaway.recent`, { user_id: uid, limit: 10 }, 2000]] : []);
});
