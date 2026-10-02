// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, mock, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let username: string | null = 'Foo';
mock.module('./services', () => ({
  accountState: async () => {
    if (username === null) throw new Error('state unavailable');
    return { username };
  }
}));

const { purgeEdge, purgeEdgeByTag, schedulePurgeChannel, channelPageUrls } = await import('./edge-purge');

type Purge = { url: string; authorization: string; body: unknown };

const realFetch = globalThis.fetch;
const realSetTimeout = globalThis.setTimeout;
const PURGE_URL = 'https://api.cloudflare.com/client/v4/zones/zone1/purge_cache';
const PAGE = 'https://commands.itsbagelbot.com/user/foo';
let purges: Purge[];
let replyStatus: number;

function stubFetch(): void {
  globalThis.fetch = (async (url: string, init: { headers: Record<string, string>; body: string }) => {
    purges.push({ url, authorization: init.headers.authorization, body: JSON.parse(init.body) });
    return new Response(null, { status: replyStatus });
  }) as unknown as typeof fetch;
}

beforeEach(() => {
  purges = [];
  replyStatus = 200;
  Object.assign(privateEnv, { CF_ZONE_ID: 'zone1', CF_CACHE_PURGE_TOKEN: 'token1' });
  stubFetch();
});

afterEach(() => {
  globalThis.fetch = realFetch;
  globalThis.setTimeout = realSetTimeout;
  jest.useRealTimers();
  for (const key of Object.keys(privateEnv)) delete privateEnv[key];
});

const calls = {
  purgeEdge: { run: () => purgeEdge([PAGE]), body: { files: [PAGE] } },
  purgeEdgeByTag: { run: () => purgeEdgeByTag('dashboard-edge-shell'), body: { tags: ['dashboard-edge-shell'] } }
};

const transport: { name: string; configured: boolean; status: number; want: { ok: boolean; sent: boolean } }[] = [
  { name: 'unset zone/token returns false without a network call', configured: false, status: 200, want: { ok: false, sent: false } },
  { name: 'a non-2xx reply returns false', configured: true, status: 500, want: { ok: false, sent: true } },
  { name: 'a 2xx reply returns true', configured: true, status: 200, want: { ok: true, sent: true } }
];

describe.each(Object.keys(calls) as (keyof typeof calls)[])('%s', (name) => {
  test.each(transport)('$name', async ({ configured, status, want }) => {
    if (!configured) delete privateEnv.CF_ZONE_ID;
    replyStatus = status;
    const ok = await calls[name].run();
    expect({ ok, sent: purges.length > 0 }).toEqual(want);
  });

  test('sends the zone URL, bearer auth, and the purge body', async () => {
    await calls[name].run();
    expect(purges).toEqual([{ url: PURGE_URL, authorization: 'Bearer token1', body: calls[name].body }]);
  });
});

test('an aborted (timed out) request returns false', async () => {
  jest.useFakeTimers();
  globalThis.fetch = ((_url: string, init: { signal: AbortSignal }) =>
    new Promise((_resolve, reject) => {
      init.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    })) as unknown as typeof fetch;
  const pending = purgeEdge([PAGE]);
  jest.advanceTimersByTime(3000);
  expect(await pending).toBe(false);
});

test('channelPageUrls lists every public page variant of a channel', () => {
  expect(channelPageUrls('foo')).toEqual([
    PAGE,
    'https://commands.itsbagelbot.com/user/@foo',
    'https://leaderboard.itsbagelbot.com/foo',
    'https://leaderboard.itsbagelbot.com/@foo'
  ]);
});

describe('schedulePurgeChannel', () => {
  let timers: Array<() => void>;

  beforeEach(() => {
    timers = [];
    globalThis.setTimeout = ((fn: () => void) => {
      timers.push(fn);
      return { unref: () => {} };
    }) as unknown as typeof setTimeout;
  });

  async function settle(): Promise<void> {
    for (const fire of timers.splice(0)) fire();
    await new Promise((resolve) => realSetTimeout(resolve, 0));
  }

  const channelPurge = { files: channelPageUrls('foo') };
  const cases: { name: string; username: string | null; batches: string[][]; want: unknown[] }[] = [
    {
      name: 'burst of writes coalesces into one purge of every channel page variant',
      username: 'Foo',
      batches: [['1', '1', '1']],
      want: [channelPurge]
    },
    { name: 'a write after the purge fired schedules a fresh purge', username: 'Foo', batches: [['2'], ['2']], want: [channelPurge, channelPurge] },
    { name: 'separate users do not coalesce', username: 'Foo', batches: [['3', '4']], want: [channelPurge, channelPurge] },
    { name: 'an unknown login skips the network call', username: '', batches: [['5']], want: [] },
    { name: 'a malformed login skips the network call', username: 'bad/login', batches: [['6']], want: [] },
    { name: 'an unavailable account state skips the network call', username: null, batches: [['7']], want: [] }
  ];

  test.each(cases)('$name', async ({ username: login, batches, want }) => {
    username = login;
    for (const batch of batches) {
      batch.forEach(schedulePurgeChannel);
      await settle();
    }
    expect(purges.map((purge) => purge.body)).toEqual(want);
  });
});
