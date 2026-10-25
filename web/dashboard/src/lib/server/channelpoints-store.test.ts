// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { ChannelPointReward } from '@bagel/kit';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> = {};
const rpcCalls: { subject: string; req: Record<string, unknown> }[] = [];
const upsertModule = mock(async () => {});

const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({ ...realNats, rpcReply: async (subject: string, req: Record<string, unknown>) => {
    rpcCalls.push({ subject, req });
    return reply;
  }, publish: async () => {} }));
let storedRewards: ChannelPointReward[] = [];
mock.module('./commands-store', () => ({
  listModules: async () => (storedRewards.length ? [{ name: 'channelpoints', is_enabled: true, configs: { rewards: storedRewards } }] : []),
  upsertModule
}));
mock.module('./loyalty-store', () => ({ createCounter: async () => {} }));

const { createReward, updateReward, readRewards } = await import('./channelpoints-store');

const draft = { id: '', title: 'Hydrate', cost: 100, action: 'none' } as ChannelPointReward;

describe('createReward refusals', () => {
  beforeEach(() => upsertModule.mockClear());

  test('a duplicate title comes back as a result, not a throw', async () => {
    reply = { error: 'duplicate reward title', code: 'conflict' };
    expect(await createReward('1', draft)).toEqual({ ok: false, duplicateTitle: true });
    expect(upsertModule).not.toHaveBeenCalled();
  });

  test('a non-affiliate channel comes back as a result, not a throw', async () => {
    reply = { error: 'broadcaster not affiliate', code: 'forbidden', not_affiliate: true };
    expect(await createReward('1', draft)).toEqual({ ok: false, notAffiliate: true });
    expect(upsertModule).not.toHaveBeenCalled();
  });

  test('a missing scope still reaches the reconnect CTA', async () => {
    reply = { error: 'reconnect required', code: 'forbidden', missing_scope: true };
    expect(await createReward('1', draft)).toEqual({ ok: false, missingScope: true });
  });

  test('any other refusal throws so moduleAction logs it', async () => {
    reply = { error: 'twitch request failed', code: 'internal' };
    await expect(createReward('1', draft)).rejects.toThrow('twitch request failed');
    expect(upsertModule).not.toHaveBeenCalled();
  });

  test('a created reward is written to the bindings blob', async () => {
    reply = { reward: { id: 'r1', title: 'Hydrate', cost: 100 } };
    const res = await createReward('1', draft);
    expect(res.ok).toBe(true);
    expect(upsertModule).toHaveBeenCalledTimes(1);
  });
});

describe('createReward blob write failure', () => {
  beforeEach(() => {
    upsertModule.mockClear();
    rpcCalls.length = 0;
  });

  test('deletes the created Twitch reward and rethrows the original error', async () => {
    reply = { reward: { id: 'r9', title: 'Hydrate', cost: 100 } };
    const failure = new Error('blob write failed');
    upsertModule.mockRejectedValueOnce(failure);
    await expect(createReward('1', draft)).rejects.toBe(failure);
    const del = rpcCalls.find((c) => c.subject.endsWith('.channelpoints.delete'));
    expect(del?.req).toEqual({ broadcaster_id: '1', reward_id: 'r9' });
  });
});

describe('readRewards legacy blobs', () => {
  test('a reward stored without newer keys gets blank defaults and keeps its stored fields', async () => {
    storedRewards = [{ id: 'r1', title: 'Old', cost: 250 } as ChannelPointReward];
    const { rewards } = await readRewards('1');
    expect(rewards).toHaveLength(1);
    expect(rewards[0]).toMatchObject({ id: 'r1', title: 'Old', cost: 250, counter: '', points: 0 });
    storedRewards = [];
  });
});

describe('updateReward blob upsert', () => {
  const stored = { id: 'r1', title: 'Old', cost: 50, action: 'none' } as ChannelPointReward;
  const written = () => (upsertModule.mock.calls[0] as unknown[])[3] as { rewards: ChannelPointReward[] };

  beforeEach(() => {
    upsertModule.mockClear();
    storedRewards = [stored];
  });

  test('an id missing from the blob is appended', async () => {
    reply = { reward: { id: 'r2', title: 'New', cost: 100 } };
    const res = await updateReward('1', { ...draft, id: 'r2', title: 'New' });
    expect(res.ok).toBe(true);
    expect(written().rewards.map((r) => r.id)).toEqual(['r1', 'r2']);
    storedRewards = [];
  });

  test('an id already in the blob is replaced, not duplicated', async () => {
    reply = { reward: { id: 'r1', title: 'Renamed', cost: 75 } };
    await updateReward('1', { ...draft, id: 'r1', title: 'Renamed' });
    expect(written().rewards.map((r) => [r.id, r.title])).toEqual([['r1', 'Renamed']]);
    storedRewards = [];
  });
});
