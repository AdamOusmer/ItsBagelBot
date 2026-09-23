// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// createReward must turn outgress's two actionable refusals into results the
// page renders. Through rpc() both threw before the store read them, so a
// duplicate title surfaced as the generic "Could not update" line.
import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { ChannelPointReward } from '@bagel/kit';

// The kit root drags in the i18n catalog (import.meta.glob, Vite-only); the
// store only needs MOD from it, which lives in the catalog entry.
const { MOD } = await import('@bagel/kit/catalog');
mock.module('@bagel/kit', () => ({ MOD }));

const realNats = await import('@bagel/kit/server/nats');
let reply: Record<string, unknown> = {};
const upsertModule = mock(async () => {});

mock.module('@bagel/kit/server/nats', () => ({ ...realNats, rpcReply: async () => reply }));
mock.module('./services', () => ({ SUB: { outgressRpc: 'test' }, publishEventSubEnsureOptional: async () => {} }));
mock.module('./commands-store', () => ({ upsertModule }));
mock.module('./module-blob', () => ({
  readModuleBlob: async () => ({ enabled: false, configs: {} }),
  setModuleEnabled: async () => {}
}));
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
