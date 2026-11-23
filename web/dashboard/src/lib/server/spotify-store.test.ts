// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { blankSpotifyQuotas } = await import('@bagel/kit');

let stored: Record<string, unknown> = {};
let storedIsEnabled = true;
let staleCache: Record<string, unknown> | null = null;

const commandsStore = await import('./commands-store');
mock.module('./commands-store', () => ({
  ...commandsStore,
  listModules: async () => [{ name: 'songqueue', is_enabled: storedIsEnabled, configs: staleCache ?? stored }],
  listModulesForEditing: async () => [{ name: 'songqueue', is_enabled: storedIsEnabled, configs: stored }],
  patchModule: async (p: { isEnabled: boolean; partial: Record<string, unknown> }) => {
    storedIsEnabled = p.isEnabled;
    stored = { ...stored, ...p.partial };
    return { rev: 1, conflict: false, synced: true };
  },
  patchModuleConfig: async (p: { partial: Record<string, unknown> }) => {
    stored = { ...stored, ...p.partial };
  }
}));

const rewardWire = await import('./reward-wire');
mock.module('./reward-wire', () => ({
  ...rewardWire,
  saveRewardFlow: async (_userId: string, _opts: unknown, save: (reward: any) => Promise<any>) => {
    await save({ rewardId: 'rew-new', title: 'New Reward', cost: 100 });
    return { ok: true };
  },
  deleteRemoteReward: async () => ({ ok: true })
}));

const { spotifyStore } = await import('./spotify-store');

describe('song queue length', () => {
  test.each([
    { name: 'a saved length round trips', saved: 25, written: 25, read: 25 },
    { name: 'no length is written as unlimited', saved: null, written: 0, read: null }
  ])('$name', async ({ saved, written, read }) => {
    stored = { maxDepth: 100 };
    const store = spotifyStore('1');
    await store.saveLimits({ quotas: blankSpotifyQuotas(), maxDepth: saved });
    expect({ written: stored.maxDepth, read: (await store.read()).maxDepth }).toEqual({ written, read });
  });

  test.each([
    { name: 'a legacy string length', raw: '10', read: 10 },
    { name: 'zero is unlimited', raw: 0, read: null },
    { name: 'a missing length is unlimited', raw: undefined, read: null }
  ])('reads $name', async ({ raw, read }) => {
    stored = { maxDepth: raw };
    expect((await spotifyStore('1').read()).maxDepth).toBe(read);
  });
});

describe('song queue disabled triggers', () => {
  test('normalizes disabledTriggers and preserves them across limit saves', async () => {
    stored = { maxDepth: 10 };
    const store = spotifyStore('1');
    await store.saveDisabledTriggers(' !SONG, bogus, sr ');
    expect({ written: stored.disabledTriggers, read: (await store.read()).disabledTriggers }).toEqual({
      written: 'song,sr',
      read: 'song,sr'
    });
    await store.saveLimits({ quotas: blankSpotifyQuotas(), maxDepth: 20 });
    expect({ maxDepth: stored.maxDepth, disabledTriggers: stored.disabledTriggers }).toEqual({
      maxDepth: 20,
      disabledTriggers: 'song,sr'
    });
  });
});

describe('spotify-store atomic updates and preservation', () => {
  test('saveSr and setRedeemPath ignore stale cached listModules and preserve sibling allowOffline flags', async () => {
    stored = {
      disabledTriggers: 'skip',
      quotas: { everyone: 2 },
      maxDepth: 10,
      sr: { enabled: true, perm: 'everyone', allowOffline: false },
      redeem: { enabled: true, allowOffline: true, rewardId: 'r1' }
    };
    staleCache = {
      sr: { enabled: true, perm: 'everyone', allowOffline: false },
      redeem: { enabled: false, allowOffline: false, rewardId: '' }
    };
    const store = spotifyStore('1');
    const res = await store.saveSr({ enabled: true, perm: 'sub', allowOffline: true });
    await store.setRedeemPath({ enabled: true, allowOffline: true });
    staleCache = null;
    const view = await store.read();
    expect({
      ok: res.ok,
      sr: view.sr,
      redeemOffline: view.redeem.allowOffline,
      rewardId: view.redeem.rewardId,
      maxDepth: view.maxDepth,
      disabledTriggers: view.disabledTriggers
    }).toEqual({
      ok: true,
      sr: { enabled: true, perm: 'sub', allowOffline: true },
      redeemOffline: true,
      rewardId: 'r1',
      maxDepth: 10,
      disabledTriggers: 'skip'
    });
  });

  test('setEnabled, saveLimits, and saveReward preserve allowOffline and reward metadata', async () => {
    stored = {
      sr: { enabled: true, perm: 'everyone', allowOffline: true },
      redeem: { enabled: true, allowOffline: true, rewardId: 'r1', onRedeem: 'fulfill', replyMessage: 'old' }
    };
    const store = spotifyStore('1');
    await store.setEnabled(false);
    await store.saveLimits({ quotas: blankSpotifyQuotas(), maxDepth: 5 });
    await store.saveReward({ title: 'Reward', cost: 50, color: '#ff0000', cooldown: 0, onRedeem: 'leave', replyMessage: 'queued {songqueue:track}' });
    const view = await store.read();
    expect({
      enabled: view.enabled,
      maxDepth: view.maxDepth,
      srOffline: view.sr.allowOffline,
      redeemOffline: view.redeem.allowOffline,
      rewardId: view.redeem.rewardId,
      onRedeem: view.redeem.onRedeem,
      replyMessage: view.redeem.replyMessage
    }).toEqual({
      enabled: false,
      maxDepth: 5,
      srOffline: true,
      redeemOffline: true,
      rewardId: 'rew-new',
      onRedeem: 'leave',
      replyMessage: 'queued {songqueue:track}'
    });
  });
});
