// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';

const envVars: Record<string, string | undefined> = {};
function setEnv(next: Record<string, string | undefined>): void {
  for (const k of Object.keys(envVars)) delete envVars[k];
  Object.assign(envVars, next);
}

mock.module('$app/environment', () => ({ dev: false }));
mock.module('$env/dynamic/private', () => ({ env: envVars }));

let username: string | null = 'Foo';
mock.module('./services', () => ({
  accountState: async () => {
    if (username === null) throw new Error('state unavailable');
    return { username };
  }
}));

const { purgeEdge, schedulePurgeChannel, channelPageUrls } = await import('./edge-purge');

const originalFetch = global.fetch;

describe('purgeEdge', () => {
  beforeEach(() => {
    setEnv({ CF_ZONE_ID: 'zone1', CF_CACHE_PURGE_TOKEN: 'token1' });
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  test('unset zone/token returns false without a network call', async () => {
    setEnv({});
    const fetchSpy = mock(() => Promise.resolve(new Response(null, { status: 200 })));
    global.fetch = fetchSpy as unknown as typeof fetch;

    expect(await purgeEdge(['https://commands.itsbagelbot.com/user/foo'])).toBe(false);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test('a non-2xx reply returns false', async () => {
    global.fetch = mock(() => Promise.resolve(new Response(null, { status: 500 }))) as unknown as typeof fetch;

    expect(await purgeEdge(['https://commands.itsbagelbot.com/user/foo'])).toBe(false);
  });

  test('a 2xx reply returns true', async () => {
    global.fetch = mock(() => Promise.resolve(new Response(null, { status: 200 }))) as unknown as typeof fetch;

    expect(await purgeEdge(['https://commands.itsbagelbot.com/user/foo'])).toBe(true);
  });

  test('an aborted (timed out) request returns false', async () => {
    global.fetch = mock(
      (_url: string, opts: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          opts.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
        })
    ) as unknown as typeof fetch;

    expect(await purgeEdge(['https://commands.itsbagelbot.com/user/foo'])).toBe(false);
  }, 6000);
});

describe('schedulePurgeChannel', () => {
  const originalSetTimeout = global.setTimeout;
  let timers: Array<() => void>;
  let bodies: Array<{ files: string[] }>;

  beforeEach(() => {
    setEnv({ CF_ZONE_ID: 'zone1', CF_CACHE_PURGE_TOKEN: 'token1' });
    username = 'Foo';
    timers = [];
    bodies = [];
    global.setTimeout = ((fn: () => void) => {
      timers.push(fn);
      return { unref: () => {} };
    }) as unknown as typeof setTimeout;
    global.fetch = mock((_url: string, opts: { body: string }) => {
      bodies.push(JSON.parse(opts.body));
      return Promise.resolve(new Response(null, { status: 200 }));
    }) as unknown as typeof fetch;
  });

  afterEach(() => {
    global.setTimeout = originalSetTimeout;
    global.fetch = originalFetch;
  });

  const flush = async () => {
    for (const fire of timers.splice(0)) fire();
    await new Promise((resolve) => originalSetTimeout(resolve, 0));
  };

  test('burst of writes coalesces into one purge of every channel page variant', async () => {
    schedulePurgeChannel('1');
    schedulePurgeChannel('1');
    schedulePurgeChannel('1');
    expect(timers).toHaveLength(1);

    await flush();
    expect(bodies).toEqual([{ files: channelPageUrls('foo') }]);
    expect(bodies[0].files).toEqual([
      'https://commands.itsbagelbot.com/user/foo',
      'https://commands.itsbagelbot.com/user/@foo',
      'https://leaderboard.itsbagelbot.com/foo',
      'https://leaderboard.itsbagelbot.com/@foo'
    ]);
  });

  test('a write after the purge fired schedules a fresh purge', async () => {
    schedulePurgeChannel('2');
    await flush();
    schedulePurgeChannel('2');
    await flush();
    expect(bodies).toHaveLength(2);
  });

  test('separate users do not coalesce', async () => {
    schedulePurgeChannel('3');
    schedulePurgeChannel('4');
    expect(timers).toHaveLength(2);
    await flush();
  });

  test('unknown or malformed login skips the network call', async () => {
    username = '';
    schedulePurgeChannel('5');
    await flush();
    username = 'bad/login';
    schedulePurgeChannel('6');
    await flush();
    username = null;
    schedulePurgeChannel('7');
    await flush();
    expect(bodies).toHaveLength(0);
  });
});
