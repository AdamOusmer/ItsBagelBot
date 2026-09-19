// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// purgeEdge never throws: every failure mode (unset config, refusal, timeout)
// degrades to `false` so the caller can surface edgeDelayed instead of failing
// a write that already landed.
import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';

// `env` from $env/dynamic/private is captured by reference when edge-purge.ts
// first imports it, so tests MUTATE this one object rather than reassigning
// the binding below (a reassignment would only rebind the local variable, not
// the object edge-purge.ts already destructured `env` out of).
const envVars: Record<string, string | undefined> = {};
function setEnv(next: Record<string, string | undefined>): void {
  for (const k of Object.keys(envVars)) delete envVars[k];
  Object.assign(envVars, next);
}

mock.module('$app/environment', () => ({ dev: false }));
mock.module('$env/dynamic/private', () => ({ env: envVars }));

const { purgeEdge } = await import('./edge-purge');

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
