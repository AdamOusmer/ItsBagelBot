// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { blankSpotifyQuotas } = await import('@bagel/kit');

let stored: Record<string, unknown> = {};

const commandsStore = await import('./commands-store');
mock.module('./commands-store', () => ({
  ...commandsStore,
  listModules: async () => [{ name: 'songqueue', is_enabled: true, configs: stored }],
  upsertModule: async (_userId: string, _name: string, _enabled: boolean, configs: Record<string, unknown>) => {
    stored = configs;
    return { modules: [] };
  }
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
