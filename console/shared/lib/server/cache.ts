// Bounded in-process cache: LRU eviction + stale-while-revalidate + single-flight.
//
// This is tier 1 of the read path (per-replica, microsecond hits). Freshness is
// primarily push-driven (the cache-invalidation bus), so the clock windows here
// are safety nets, not the consistency mechanism. Semantics per read:
//
//   * fresh hit   — value returned as-is.
//   * stale hit   — value returned immediately, one background revalidation is
//     kicked off (single-flighted) so the next reader sees fresh data.
//   * miss        — callers coalesce on one in-flight load.
//   * loader error — if a value exists within its stale-if-error window, it is
//     served instead of throwing (an outage degrades to slightly-old data, not
//     a broken page); otherwise the error propagates and nothing is cached.
//
// Correctness details this design fixes over the previous MemoryCache:
//
//   * in-flight loads live in a separate map keyed independently of TTL, so a
//     load that outlives its entry's expiry still deduplicates concurrent
//     callers (previously two loads could run and their commits raced);
//   * every key carries a generation counter, bumped by invalidate/set/delete.
//     A load only commits if the generation it started under is still current,
//     so a bus invalidation (or optimistic write) always beats an older
//     in-flight result — stale data can never resurrect a just-evicted key.
//
// Values are heterogeneous (one cache per app holds tiers, accounts, commands,
// ...), so entries are stored as `unknown` and typed at the call site via the
// generic on getOrLoad/set.

export interface CachePolicy {
  /** How long a committed value is served without any refresh. */
  freshMs: number;
  /** Additional window after freshMs where the value is served stale while one
   *  background revalidation runs. 0 disables SWR (hard TTL). */
  swrMs?: number;
  /** Window after freshMs where a FAILED load falls back to the last value
   *  instead of throwing. Defaults to swrMs. */
  staleIfErrorMs?: number;
}

export type CacheEvent =
  | 'hit'
  | 'stale'
  | 'miss'
  | 'revalidate'
  | 'error_served_stale'
  | 'invalidate';

export interface SwrCacheOptions {
  /** Max live entries before LRU eviction. Default 5000. */
  capacity?: number;
  /** Metrics tap; called synchronously per event with the affected key. */
  onEvent?: (event: CacheEvent, key: string) => void;
  /** Called when a loader failure is masked by stale-serving (observability). */
  onMaskedError?: (err: unknown, key: string) => void;
}

interface Entry {
  value: unknown;
  freshUntil: number;
  staleUntil: number;
  errorUntil: number;
}

interface Inflight {
  promise: Promise<unknown>;
  gen: number;
}

function normalize(policy: CachePolicy | number): Required<CachePolicy> {
  if (typeof policy === 'number') return { freshMs: policy, swrMs: 0, staleIfErrorMs: 0 };
  const swrMs = policy.swrMs ?? 0;
  return { freshMs: policy.freshMs, swrMs, staleIfErrorMs: policy.staleIfErrorMs ?? swrMs };
}

export class SwrCache {
  // Map preserves insertion order; we treat it as an LRU by re-inserting on hit.
  private readonly store = new Map<string, Entry>();
  private readonly inflight = new Map<string, Inflight>();
  // Generation per key. Missing key = generation 0. Cleaned up lazily (see gc).
  private readonly generations = new Map<string, number>();
  private readonly capacity: number;
  private readonly onEvent?: (event: CacheEvent, key: string) => void;
  private readonly onMaskedError?: (err: unknown, key: string) => void;

  constructor(opts: SwrCacheOptions = {}) {
    this.capacity = opts.capacity ?? 5000;
    this.onEvent = opts.onEvent;
    this.onMaskedError = opts.onMaskedError;
  }

  /**
   * Return the cached value for `key` under `policy` (a CachePolicy, or a bare
   * number treated as a hard TTL in ms), or run `load` (deduped across
   * concurrent callers) and cache the result. See module docs for the fresh /
   * stale / error semantics.
   */
  async getOrLoad<T>(key: string, policy: CachePolicy | number, load: () => Promise<T>): Promise<T> {
    const p = normalize(policy);
    const now = Date.now();
    const entry = this.store.get(key);

    if (entry && now < entry.freshUntil) {
      this.touch(key, entry);
      this.emit('hit', key);
      return entry.value as T;
    }

    if (entry && now < entry.staleUntil) {
      this.emit('stale', key);
      this.revalidate(key, p, load);
      return entry.value as T;
    }

    this.emit('miss', key);
    return this.loadShared(key, p, load);
  }

  /** Overwrite `key` with a known value (e.g. after an optimistic write). Bumps
   *  the generation so any older in-flight load cannot clobber it. */
  set<T>(key: string, value: T, policy: CachePolicy | number): void {
    this.doom(key);
    this.commit(key, value, normalize(policy));
  }

  /** Drop a single key (and doom any in-flight load for it). */
  delete(key: string): void {
    this.doom(key);
    this.store.delete(key);
    this.emit('invalidate', key);
  }

  /** Drop every key that starts with any of the given prefixes. In-flight loads
   *  for matching keys are doomed too: their commits are discarded AND they are
   *  deregistered, so a reader arriving after the invalidation starts a fresh
   *  load instead of coalescing onto the pre-invalidation one. */
  invalidate(...prefixes: string[]): void {
    const matches = (key: string) => prefixes.some((p) => key.startsWith(p));
    for (const key of this.store.keys()) {
      if (matches(key)) {
        this.doom(key);
        this.store.delete(key);
        this.emit('invalidate', key);
      }
    }
    for (const key of this.inflight.keys()) {
      if (matches(key)) this.doom(key);
    }
  }

  clear(): void {
    // Doom every in-flight load, then drop all values.
    for (const key of this.inflight.keys()) this.doom(key);
    for (const key of this.store.keys()) this.doom(key);
    this.store.clear();
    this.emit('invalidate', '*');
  }

  get size(): number {
    return this.store.size;
  }

  private gen(key: string): number {
    return this.generations.get(key) ?? 0;
  }

  /** Invalidate `key`'s identity: bump the generation (discarding any in-flight
   *  commit) and deregister the in-flight load so new readers start fresh. */
  private doom(key: string): void {
    this.bump(key);
    this.inflight.delete(key);
  }

  private bump(key: string): void {
    this.generations.set(key, this.gen(key) + 1);
    // Bound the generations map: entries are only meaningful while a load is in
    // flight or a value is stored; sweep orphans once the map runs well past
    // the value store's own bound.
    if (this.generations.size > this.capacity * 2) {
      for (const k of this.generations.keys()) {
        if (!this.store.has(k) && !this.inflight.has(k)) this.generations.delete(k);
      }
    }
  }

  /** Deduped load. Reuses an in-flight promise when present; otherwise starts
   *  one tagged with the key's current generation. */
  private loadShared<T>(key: string, p: Required<CachePolicy>, load: () => Promise<T>): Promise<T> {
    const existing = this.inflight.get(key);
    if (existing) return existing.promise as Promise<T>;

    const gen = this.gen(key);
    const promise = (async (): Promise<T> => {
      try {
        const value = await load();
        if (this.gen(key) === gen) this.commit(key, value, p);
        return value;
      } catch (err) {
        const cur = this.store.get(key);
        if (cur && Date.now() < cur.errorUntil) {
          this.emit('error_served_stale', key);
          this.onMaskedError?.(err, key);
          return cur.value as T;
        }
        throw err;
      } finally {
        // Remove our own registration. Comparing by generation (captured before
        // the load started) instead of promise identity sidesteps TS's
        // used-before-assigned analysis on self-referencing closures; a newer
        // load for this key can only exist under a bumped generation.
        const inflight = this.inflight.get(key);
        if (inflight && inflight.gen === gen) this.inflight.delete(key);
      }
    })();

    this.inflight.set(key, { promise, gen });
    return promise;
  }

  /** Background refresh behind a stale hit. Single-flighted via loadShared;
   *  failures are already downgraded to stale-serving there, and a failure past
   *  the error window only affects this background task (the stale value was
   *  already returned to the caller), so it is swallowed. */
  private revalidate<T>(key: string, p: Required<CachePolicy>, load: () => Promise<T>): void {
    if (this.inflight.has(key)) return;
    this.emit('revalidate', key);
    this.loadShared(key, p, load).catch(() => {});
  }

  private commit(key: string, value: unknown, p: Required<CachePolicy>): void {
    const now = Date.now();
    this.insert(key, {
      value,
      freshUntil: now + p.freshMs,
      staleUntil: now + p.freshMs + p.swrMs,
      errorUntil: now + p.freshMs + Math.max(p.staleIfErrorMs, p.swrMs)
    });
  }

  private emit(event: CacheEvent, key: string): void {
    try {
      this.onEvent?.(event, key);
    } catch {
      // Metrics must never break the read path.
    }
  }

  private touch(key: string, entry: Entry): void {
    // Move to most-recently-used by re-inserting at the tail.
    this.store.delete(key);
    this.store.set(key, entry);
  }

  private insert(key: string, entry: Entry): void {
    this.store.delete(key);
    this.store.set(key, entry);
    while (this.store.size > this.capacity) {
      // Evict least-recently-used (the first/oldest key).
      const oldest = this.store.keys().next().value;
      if (oldest === undefined) break;
      this.store.delete(oldest);
    }
  }
}
