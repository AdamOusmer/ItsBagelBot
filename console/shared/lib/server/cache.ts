// Bounded in-process cache: LRU eviction + per-entry TTL + single-flight.
//
// This is tier 1 of the read path (per-replica, microsecond hits). It replaces
// the unbounded `Map` each app's rpc.ts grew independently, fixing three things:
//   * memory: capacity-bounded LRU instead of an ever-growing map;
//   * thundering herd: concurrent cold reads of the same key share one promise;
//   * correctness on failure: a rejected loader drops the key (no negative cache).
//
// Values are heterogeneous (one cache per app holds tiers, accounts, commands,
// ...), so entries are stored as `unknown` and typed at the call site via the
// generic on getOrLoad/set.

interface Entry {
  value?: unknown;
  promise?: Promise<unknown>;
  expires: number;
}

export interface MemoryCacheOptions {
  /** Max live entries before LRU eviction. Default 5000. */
  capacity?: number;
}

export class MemoryCache {
  // Map preserves insertion order; we treat it as an LRU by re-inserting on hit.
  private readonly store = new Map<string, Entry>();
  private readonly capacity: number;

  constructor(opts: MemoryCacheOptions = {}) {
    this.capacity = opts.capacity ?? 5000;
  }

  /**
   * Return the cached value for `key`, or run `load` (deduped across concurrent
   * callers) and cache it for `ttlMs`. A rejected `load` removes the key so the
   * next caller retries instead of caching the failure.
   */
  async getOrLoad<T>(key: string, ttlMs: number, load: () => Promise<T>): Promise<T> {
    const now = Date.now();
    const hit = this.store.get(key);
    if (hit && hit.expires > now) {
      this.touch(key, hit);
      if (hit.value !== undefined) return hit.value as T;
      if (hit.promise) return hit.promise as Promise<T>;
    }

    const promise = load();
    this.insert(key, { promise, expires: now + ttlMs });
    try {
      const value = await promise;
      // Re-check the entry is still ours (not invalidated mid-flight) before
      // committing the resolved value.
      const cur = this.store.get(key);
      if (cur && cur.promise === promise) {
        this.insert(key, { value, expires: Date.now() + ttlMs });
      }
      return value;
    } catch (err) {
      const cur = this.store.get(key);
      if (cur && cur.promise === promise) this.store.delete(key);
      throw err;
    }
  }

  /** Overwrite `key` with a known value (e.g. after an optimistic write). */
  set<T>(key: string, value: T, ttlMs: number): void {
    this.insert(key, { value, expires: Date.now() + ttlMs });
  }

  /** Drop a single key. */
  delete(key: string): void {
    this.store.delete(key);
  }

  /** Drop every key that starts with any of the given prefixes. */
  invalidate(...prefixes: string[]): void {
    for (const key of this.store.keys()) {
      if (prefixes.some((p) => key.startsWith(p))) this.store.delete(key);
    }
  }

  clear(): void {
    this.store.clear();
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
