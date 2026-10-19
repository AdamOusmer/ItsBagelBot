// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, mock, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let username: string | null = 'Foo';
let perUser = false;
mock.module('./services', () => ({
  accountState: async (id: string) => {
    if (username === null) throw new Error('state unavailable');
    return { username: perUser ? `user${id}` : username };
  }
}));

const { purgeChannelPages, purgeEdgeByTag, schedulePurgeChannel } = await import('./edge-purge');
const { channelPageUrls, enumerableChannelQuery } = await import('./channel-urls');

type Purge = { url: string; authorization: string; body: { files?: string[]; tags?: string[] } };

const realFetch = globalThis.fetch;
const realSetTimeout = globalThis.setTimeout;
const PURGE_URL = 'https://api.cloudflare.com/client/v4/zones/zone1/purge_cache';
let purges: Purge[];
let replies: number[];

function stubFetch(): void {
  globalThis.fetch = (async (url: string, init: { headers: Record<string, string>; body: string }) => {
    purges.push({ url, authorization: init.headers.authorization, body: JSON.parse(init.body) });
    return new Response(null, { status: replies.shift() ?? 200 });
  }) as unknown as typeof fetch;
}

let timers: Array<() => void>;

function stubTimers(): void {
  timers = [];
  globalThis.setTimeout = ((fn: () => void) => {
    timers.push(fn);
    return { unref: () => {} };
  }) as unknown as typeof setTimeout;
}

async function settle(): Promise<void> {
  for (let round = 0; round < 5; round++) {
    await new Promise((resolve) => realSetTimeout(resolve, 0));
    for (const fire of timers.splice(0)) fire();
  }
  await new Promise((resolve) => realSetTimeout(resolve, 0));
}

beforeEach(() => {
  purges = [];
  replies = [];
  username = 'Foo';
  perUser = false;
  Object.assign(privateEnv, { CF_ZONE_ID: 'zone1', CF_CACHE_PURGE_TOKEN: 'token1' });
  stubFetch();
  stubTimers();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  globalThis.setTimeout = realSetTimeout;
  jest.useRealTimers();
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
});

describe('purgeEdgeByTag', () => {
  const transport: { name: string; configured: boolean; status: number; want: { ok: boolean; sent: boolean } }[] = [
    { name: 'unset zone/token returns false without a network call', configured: false, status: 200, want: { ok: false, sent: false } },
    { name: 'a non-2xx reply returns false', configured: true, status: 500, want: { ok: false, sent: true } },
    { name: 'a 2xx reply returns true', configured: true, status: 200, want: { ok: true, sent: true } }
  ];

  test.each(transport)('$name', async ({ configured, status, want }) => {
    if (!configured) delete privateEnv.CF_ZONE_ID;
    replies = [status];
    const ok = await purgeEdgeByTag('dashboard-edge-shell');
    expect({ ok, sent: purges.length > 0 }).toEqual(want);
  });

  test('sends the zone URL, bearer auth and a tags body', async () => {
    await purgeEdgeByTag('dashboard-edge-shell');
    expect(purges).toEqual([{ url: PURGE_URL, authorization: 'Bearer token1', body: { tags: ['dashboard-edge-shell'] } }]);
  });

  test('an aborted (timed out) request returns false', async () => {
    globalThis.setTimeout = realSetTimeout;
    jest.useFakeTimers();
    globalThis.fetch = ((_url: string, init: { signal: AbortSignal }) =>
      new Promise((_resolve, reject) => {
        init.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
      })) as unknown as typeof fetch;
    const pending = purgeEdgeByTag('dashboard-edge-shell');
    jest.advanceTimersByTime(3000);
    expect(await pending).toBe(false);
  });
});

describe('channelPageUrls', () => {
  test('lists every cached spelling, host and page-data variant of a channel', () => {
    const urls = channelPageUrls('42', ['foo']);
    expect(urls.filter((u) => !u.includes('__data.json'))).toEqual([
      'https://commands.itsbagelbot.com/user/foo',
      'https://commands.itsbagelbot.com/user/@foo',
      'https://commands.itsbagelbot.com/user/42',
      'https://leaderboard.itsbagelbot.com/foo',
      'https://leaderboard.itsbagelbot.com/foo?page=2',
      'https://leaderboard.itsbagelbot.com/foo?page=3',
      'https://leaderboard.itsbagelbot.com/@foo',
      'https://leaderboard.itsbagelbot.com/@foo?page=2',
      'https://leaderboard.itsbagelbot.com/@foo?page=3'
    ]);
    expect(urls.filter((u) => u.startsWith('https://leaderboard.itsbagelbot.com/foo/__data.json'))).toEqual([
      'https://leaderboard.itsbagelbot.com/foo/__data.json?x-sveltekit-invalidated=001',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?x-sveltekit-invalidated=011',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?x-sveltekit-invalidated=101',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?x-sveltekit-invalidated=111',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=2&x-sveltekit-invalidated=001',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=2&x-sveltekit-invalidated=011',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=2&x-sveltekit-invalidated=101',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=2&x-sveltekit-invalidated=111',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=3&x-sveltekit-invalidated=001',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=3&x-sveltekit-invalidated=011',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=3&x-sveltekit-invalidated=101',
      'https://leaderboard.itsbagelbot.com/foo/__data.json?page=3&x-sveltekit-invalidated=111'
    ]);
  });

  test('every listed URL is one the hooks may share, and nothing else on these routes is', () => {
    const routeOf = (u: URL) => (u.hostname.startsWith('commands.') ? '/(public)/user/[channel]' : '/(public)/[user]');
    const shareable = channelPageUrls('42', ['foo']).map((raw) => {
      const u = new URL(raw);
      return enumerableChannelQuery(routeOf(u), u.search, u.pathname.endsWith('/__data.json'));
    });
    expect(shareable.every(Boolean)).toBe(true);
    expect(enumerableChannelQuery('/(public)/[user]', '?page=4', false)).toBe(false);
    expect(enumerableChannelQuery('/(public)/[user]', '?q=fan', false)).toBe(false);
    expect(enumerableChannelQuery('/(public)/user/[channel]', '?page=2', false)).toBe(false);
  });
});

describe('purgeChannelPages', () => {
  test('purges every page URL of the current login by single-file purge', async () => {
    expect(await purgeChannelPages('42')).toBe(true);
    expect(purges.map((p) => p.body)).toEqual([{ files: channelPageUrls('42', ['foo']) }]);
  });

  test('a 429 is retried once after a backoff', async () => {
    replies = [429, 200];
    const pending = purgeChannelPages('42');
    await settle();
    expect(await pending).toBe(true);
    expect(purges.length).toBe(2);
  });

  test('a second 429 fails the purge', async () => {
    replies = [429, 429];
    const pending = purgeChannelPages('42');
    await settle();
    expect(await pending).toBe(false);
  });

  test('an unavailable account state still purges the id pages', async () => {
    username = null;
    await purgeChannelPages('42');
    expect(purges.map((p) => p.body)).toEqual([{ files: channelPageUrls('42', []) }]);
  });
});

describe('schedulePurgeChannel', () => {
  test('writes across channels coalesce into batches of at most 100 URLs', async () => {
    perUser = true;
    for (const id of ['1', '1', '2', '3', '4', '5']) schedulePurgeChannel(id);
    await settle();
    const sent = purges.flatMap((p) => p.body.files ?? []);
    expect(purges.map((p) => p.body.files?.length)).toEqual([100, 100, 25]);
    expect(new Set(sent)).toEqual(new Set(['1', '2', '3', '4', '5'].flatMap((id) => channelPageUrls(id, [`user${id}`]))));
  });

  test('logins passed by the caller (a rename) are purged instead of the cached account login', async () => {
    username = 'stale';
    schedulePurgeChannel('8', ['newname', 'OldName']);
    await settle();
    expect(purges.map((p) => p.body)).toEqual([{ files: channelPageUrls('8', ['newname', 'oldname']) }]);
  });

  test('a write after the flush schedules a fresh purge', async () => {
    schedulePurgeChannel('9');
    await settle();
    schedulePurgeChannel('9');
    await settle();
    expect(purges.length).toBe(2);
  });
});
