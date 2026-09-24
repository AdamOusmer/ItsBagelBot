// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { SwrCache, type SwrCacheOptions } from './cache';

const tick = () => new Promise((r) => setTimeout(r, 0));

function make(opts: SwrCacheOptions = {}) {
  let t = 0;
  const cache = new SwrCache({ ...opts, now: () => t });
  return { cache, advance: (ms: number) => (t += ms) };
}

describe('SwrCache', () => {
  test('fresh hit returns cached value without reloading', async () => {
    const cache = new SwrCache();
    let loads = 0;
    const load = async () => ++loads;
    expect(await cache.getOrLoad('k', { freshMs: 1000 }, load)).toBe(1);
    expect(await cache.getOrLoad('k', { freshMs: 1000 }, load)).toBe(1);
    expect(loads).toBe(1);
  });

  test('single-flight: concurrent cold readers share one load', async () => {
    const cache = new SwrCache();
    let loads = 0;
    const load = async () => {
      loads++;
      await tick();
      return 'v';
    };
    const [a, b, c] = await Promise.all([
      cache.getOrLoad('k', 1000, load),
      cache.getOrLoad('k', 1000, load),
      cache.getOrLoad('k', 1000, load)
    ]);
    expect([a, b, c]).toEqual(['v', 'v', 'v']);
    expect(loads).toBe(1);
  });

  test('single-flight survives entry expiry mid-flight', async () => {
    const { cache, advance } = make();
    let loads = 0;
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const load = async () => {
      loads++;
      await gate;
      return loads;
    };
    const first = cache.getOrLoad('k', { freshMs: 1 }, load);
    advance(10);
    const second = cache.getOrLoad('k', { freshMs: 1 }, load);
    release();
    expect(await first).toBe(1);
    expect(await second).toBe(1);
    expect(loads).toBe(1);
  });

  test('invalidation mid-flight dooms the commit (generation check)', async () => {
    const cache = new SwrCache();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const slowLoad = async () => {
      await gate;
      return 'old';
    };
    const inFlight = cache.getOrLoad('k', 1000, slowLoad);
    cache.invalidate('k');
    release();
    expect(await inFlight).toBe('old');
    const next = await cache.getOrLoad('k', 1000, async () => 'new');
    expect(next).toBe('new');
  });

  test('reader arriving after invalidation does not coalesce onto the doomed load', async () => {
    const cache = new SwrCache();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const doomed = cache.getOrLoad('k', 1000, async () => {
      await gate;
      return 'pre-invalidation';
    });
    cache.invalidate('k');
    const fresh = cache.getOrLoad('k', 1000, async () => 'post-invalidation');
    release();
    expect(await doomed).toBe('pre-invalidation');
    expect(await fresh).toBe('post-invalidation');
  });

  test('set() beats an older in-flight load', async () => {
    const cache = new SwrCache();
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const inFlight = cache.getOrLoad('k', 1000, async () => {
      await gate;
      return 'loader';
    });
    cache.set('k', 'optimistic', 1000);
    release();
    await inFlight;
    expect(await cache.getOrLoad('k', 1000, async () => 'reload')).toBe('optimistic');
  });

  test('stale-while-revalidate serves stale immediately and refreshes in background', async () => {
    const { cache, advance } = make();
    let loads = 0;
    const load = async () => `v${++loads}`;
    const policy = { freshMs: 5, swrMs: 10_000 };
    expect(await cache.getOrLoad('k', policy, load)).toBe('v1');
    advance(15);
    expect(await cache.getOrLoad('k', policy, load)).toBe('v1');
    await tick();
    expect(await cache.getOrLoad('k', policy, load)).toBe('v2');
    expect(loads).toBe(2);
  });

  test('stale-if-error serves last known value when the loader fails', async () => {
    const cache = new SwrCache();
    const masked: string[] = [];
    const { cache: observed, advance } = make({ onMaskedError: (_e, key) => masked.push(key) });
    const policy = { freshMs: 5, swrMs: 0, staleIfErrorMs: 10_000 };
    expect(await observed.getOrLoad('k', policy, async () => 'known')).toBe('known');
    advance(15);
    const v = await observed.getOrLoad('k', policy, async () => {
      throw new Error('rpc down');
    });
    expect(v).toBe('known');
    expect(masked).toEqual(['k']);
    void cache;
  });

  test('loader failure with no stale value propagates and caches nothing', async () => {
    const cache = new SwrCache();
    await expect(
      cache.getOrLoad('k', 1000, async () => {
        throw new Error('boom');
      })
    ).rejects.toThrow('boom');
    expect(await cache.getOrLoad('k', 1000, async () => 'recovered')).toBe('recovered');
  });

  test('prefix invalidate drops matching keys and dooms matching in-flight loads', async () => {
    const cache = new SwrCache();
    cache.set('commands:1', ['a'], 1000);
    cache.set('commands:2', ['b'], 1000);
    cache.set('modules:1', ['m'], 1000);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const inFlight = cache.getOrLoad('commands:3', 1000, async () => {
      await gate;
      return ['c'];
    });
    cache.invalidate('commands:');
    release();
    await inFlight;
    expect(await cache.getOrLoad('commands:1', 1000, async () => ['a2'])).toEqual(['a2']);
    expect(await cache.getOrLoad('commands:3', 1000, async () => ['c2'])).toEqual(['c2']);
    expect(await cache.getOrLoad('modules:1', 1000, async () => ['m2'])).toEqual(['m']);
  });

  test('clear() dooms everything (gap flush)', async () => {
    const cache = new SwrCache();
    cache.set('a', 1, 60_000);
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const inFlight = cache.getOrLoad('b', 60_000, async () => {
      await gate;
      return 'pre-gap';
    });
    cache.clear();
    release();
    await inFlight;
    expect(await cache.getOrLoad('a', 1000, async () => 2)).toBe(2);
    expect(await cache.getOrLoad('b', 1000, async () => 'post-gap')).toBe('post-gap');
  });

  test('LRU eviction respects capacity', async () => {
    const cache = new SwrCache({ capacity: 2 });
    cache.set('a', 1, 60_000);
    cache.set('b', 2, 60_000);
    cache.set('c', 3, 60_000);
    expect(cache.size).toBe(2);
    expect(await cache.getOrLoad('a', 1000, async () => 'reloaded')).toBe('reloaded');
  });

  test('numeric policy behaves as a hard TTL (no swr, no stale-if-error)', async () => {
    const { cache, advance } = make();
    let loads = 0;
    expect(await cache.getOrLoad('k', 5, async () => ++loads)).toBe(1);
    advance(15);
    expect(await cache.getOrLoad('k', 5, async () => ++loads)).toBe(2);
    await expect(
      cache.getOrLoad('other', 5, async () => {
        throw new Error('no stale to serve');
      })
    ).rejects.toThrow();
  });

  test('emits events for hit/stale/miss/revalidate', async () => {
    const events: string[] = [];
    const { cache, advance } = make({ onEvent: (e) => events.push(e) });
    const policy = { freshMs: 5, swrMs: 10_000 };
    await cache.getOrLoad('k', policy, async () => 1);
    await cache.getOrLoad('k', policy, async () => 1);
    advance(15);
    await cache.getOrLoad('k', policy, async () => 2);
    expect(events).toContain('miss');
    expect(events).toContain('hit');
    expect(events).toContain('stale');
    expect(events).toContain('revalidate');
  });
});
