// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface CachePolicy {
  freshMs: number;
  swrMs?: number;
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
  capacity?: number;
  onEvent?: (event: CacheEvent, key: string) => void;
  onMaskedError?: (err: unknown, key: string) => void;
  now?: () => number;
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
  private readonly store = new Map<string, Entry>();
  private readonly inflight = new Map<string, Inflight>();
  private readonly generations = new Map<string, number>();
  private readonly capacity: number;
  private readonly onEvent?: (event: CacheEvent, key: string) => void;
  private readonly onMaskedError?: (err: unknown, key: string) => void;
  private readonly now: () => number;

  constructor(opts: SwrCacheOptions = {}) {
    this.capacity = opts.capacity ?? 5000;
    this.onEvent = opts.onEvent;
    this.onMaskedError = opts.onMaskedError;
    this.now = opts.now ?? Date.now;
  }

  async getOrLoad<T>(key: string, policy: CachePolicy | number, load: () => Promise<T>): Promise<T> {
    const p = normalize(policy);
    const now = this.now();
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

  set<T>(key: string, value: T, policy: CachePolicy | number): void {
    this.doom(key);
    this.commit(key, value, normalize(policy));
  }

  delete(key: string): void {
    this.doom(key);
    this.store.delete(key);
    this.emit('invalidate', key);
  }

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

  private doom(key: string): void {
    this.bump(key);
    this.inflight.delete(key);
  }

  private bump(key: string): void {
    this.generations.set(key, this.gen(key) + 1);
    if (this.generations.size > this.capacity * 2) {
      for (const k of this.generations.keys()) {
        if (!this.store.has(k) && !this.inflight.has(k)) this.generations.delete(k);
      }
    }
  }

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
        if (cur && this.now() < cur.errorUntil) {
          this.emit('error_served_stale', key);
          this.onMaskedError?.(err, key);
          return cur.value as T;
        }
        throw err;
      } finally {
        const inflight = this.inflight.get(key);
        if (inflight && inflight.gen === gen) this.inflight.delete(key);
      }
    })();

    this.inflight.set(key, { promise, gen });
    return promise;
  }

  private revalidate<T>(key: string, p: Required<CachePolicy>, load: () => Promise<T>): void {
    if (this.inflight.has(key)) return;
    this.emit('revalidate', key);
    this.loadShared(key, p, load).catch(() => {});
  }

  private commit(key: string, value: unknown, p: Required<CachePolicy>): void {
    const now = this.now();
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
    } catch {}
  }

  private touch(key: string, entry: Entry): void {
    this.store.delete(key);
    this.store.set(key, entry);
  }

  private insert(key: string, entry: Entry): void {
    this.store.delete(key);
    this.store.set(key, entry);
    while (this.store.size > this.capacity) {
      const oldest = this.store.keys().next().value;
      if (oldest === undefined) break;
      this.store.delete(oldest);
    }
  }
}
