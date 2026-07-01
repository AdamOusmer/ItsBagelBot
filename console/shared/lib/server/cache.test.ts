import { describe, expect, test } from 'bun:test';
import { SwrCache } from './cache';

const tick = () => new Promise((r) => setTimeout(r, 0));
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

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

  // Regression for the MemoryCache race: the in-flight promise lived inside the
  // TTL'd entry, so once the entry expired a second caller started a duplicate
  // load. In-flight loads must dedupe regardless of entry expiry.
  test('single-flight survives entry expiry mid-flight', async () => {
    const cache = new SwrCache();
    let loads = 0;
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const load = async () => {
      loads++;
      await gate;
      return loads;
    };
    // freshMs 1: the (empty) window is over well before the load resolves.
    const first = cache.getOrLoad('k', { freshMs: 1 }, load);
    await sleep(10);
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
    cache.invalidate('k'); // bus invalidation while the load is in flight
    release();
    expect(await inFlight).toBe('old'); // caller still gets its value...
    // ...but the cache must NOT have resurrected it:
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
    // A reader AFTER the invalidation must start a fresh load, not await the
    // doomed one (which carries pre-invalidation data).
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
    const cache = new SwrCache();
    let loads = 0;
    const load = async () => `v${++loads}`;
    const policy = { freshMs: 5, swrMs: 10_000 };
    expect(await cache.getOrLoad('k', policy, load)).toBe('v1');
    await sleep(15); // past fresh, inside swr
    expect(await cache.getOrLoad('k', policy, load)).toBe('v1'); // stale served
    await tick(); // let the background revalidation commit
    expect(await cache.getOrLoad('k', policy, load)).toBe('v2'); // fresh now
    expect(loads).toBe(2);
  });

  test('stale-if-error serves last known value when the loader fails', async () => {
    const cache = new SwrCache();
    const masked: string[] = [];
    const observed = new SwrCache({ onMaskedError: (_e, key) => masked.push(key) });
    const policy = { freshMs: 5, swrMs: 0, staleIfErrorMs: 10_000 };
    expect(await observed.getOrLoad('k', policy, async () => 'known')).toBe('known');
    await sleep(15); // past fresh AND past swr (0) but inside the error window
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
    expect(await cache.getOrLoad('modules:1', 1000, async () => ['m2'])).toEqual(['m']); // untouched
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
    cache.set('c', 3, 60_000); // evicts a
    expect(cache.size).toBe(2);
    expect(await cache.getOrLoad('a', 1000, async () => 'reloaded')).toBe('reloaded');
  });

  test('numeric policy behaves as a hard TTL (no swr, no stale-if-error)', async () => {
    const cache = new SwrCache();
    let loads = 0;
    expect(await cache.getOrLoad('k', 5, async () => ++loads)).toBe(1);
    await sleep(15);
    expect(await cache.getOrLoad('k', 5, async () => ++loads)).toBe(2);
    await expect(
      cache.getOrLoad('other', 5, async () => {
        throw new Error('no stale to serve');
      })
    ).rejects.toThrow();
  });

  test('emits events for hit/stale/miss/revalidate', async () => {
    const events: string[] = [];
    const cache = new SwrCache({ onEvent: (e) => events.push(e) });
    const policy = { freshMs: 5, swrMs: 10_000 };
    await cache.getOrLoad('k', policy, async () => 1); // miss
    await cache.getOrLoad('k', policy, async () => 1); // hit
    await sleep(15);
    await cache.getOrLoad('k', policy, async () => 2); // stale + revalidate
    expect(events).toContain('miss');
    expect(events).toContain('hit');
    expect(events).toContain('stale');
    expect(events).toContain('revalidate');
  });
});
