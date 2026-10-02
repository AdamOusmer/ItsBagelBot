// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { ChannelPointReward } from '@bagel/kit';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> = {};
const upsertModule = mock(async () => {});

const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({ ...realNats, rpcReply: async () => reply, publish: async () => {} }));
mock.module('./commands-store', () => ({ listModules: async () => [], upsertModule }));
mock.module('./loyalty-store', () => ({ createCounter: async () => {} }));

const { createReward } = await import('./channelpoints-store');

const draft = { id: '', title: 'Hydrate', cost: 100, action: 'none' } as ChannelPointReward;

describe('createReward refusals', () => {
  beforeEach(() => upsertModule.mockClear());

  test('a duplicate title comes back as a result, not a throw', async () => {
    reply = { error: 'duplicate reward title', code: 'conflict' };
    expect(await createReward('1', draft)).toEqual({ ok: false, duplicateTitle: true });
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
