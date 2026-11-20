// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const bus = await import('@bagel/kit/server/nats');
const { liveDraws, mutateRaffle } = await import('./channel-raffles');
const userId = '42';
const actor = { userId, actorId: '95' };
const round_id = '32ff6551-4646-4fb4-bf55-02268d943b1a';
const round = { id: round_id, phase: 'open' as const, closes_ms: 5000, entries: 19 };
const status = { accrual_health: 'healthy', rounds: [round], unpaid: ['viewer'] };

afterEach(() => mock.restore());

test.each([false, true])('live draws preserve giveaway results when raffle status is unavailable ($available)', async (available) => {
  const rpc = spyOn(bus, 'rpc').mockResolvedValueOnce({ rounds: [] });
  if (available) rpc.mockResolvedValueOnce(status);
  else rpc.mockRejectedValueOnce(new Error('timeout'));
  expect(await liveDraws(userId)).toEqual({
    rounds: [], degraded: false,
    raffles: available
      ? { rounds: [{ ...round, kind: 'raffle' }], unpaid: ['viewer'], degraded: false }
      : { rounds: [], unpaid: [], degraded: true }
  });
  expect(rpc).toHaveBeenCalledWith('rpc.draw.raffle.status', { user_id: userId, source: 'dashboard' }, 4000);
});

const launch = { verb: 'start', input: { amount: 5000, seconds: 60, winners: 0, auto: true, split: true } } as const;

test.each([
  { mutation: launch, reply: { round_id, closes_ms: 5000, amount: 5000, winners: 3, auto: true } },
  { mutation: { verb: 'draw', input: { round_id, winners: 4, auto: false } }, reply: { done: true } },
  { mutation: { verb: 'cancel', input: { round_id } }, reply: { done: true } }
] as const)('sends raffle mutation $mutation and returns the reply', async ({ mutation, reply }) => {
  const rpc = spyOn(bus, 'rpcReply').mockResolvedValueOnce(reply);
  expect(await mutateRaffle(actor, mutation)).toEqual(reply);
  expect(rpc).toHaveBeenCalledWith(`rpc.draw.raffle.${mutation.verb}`, {
    user_id: userId, actor_id: actor.actorId, ...mutation.input
  }, 10_000);
});

test.each([
  { response: { error: 'raffle already open', code: 'conflict', reason: 'busy_raffle' },
    expected: { reason: 'busy_raffle', refusal: { code: 'conflict' } } },
  { response: {}, expected: { message: 'Raffle mutation returned no completion' } }
])('rejects an incomplete or refused raffle start ($response)', async ({ response, expected }) => {
  spyOn(bus, 'rpcReply').mockResolvedValueOnce(response);
  await expect(mutateRaffle(actor, launch)).rejects.toMatchObject(expected);
});
