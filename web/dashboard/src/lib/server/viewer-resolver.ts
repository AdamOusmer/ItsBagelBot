// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const FOUND_TTL_MS = 10 * 60_000;
export const MISSING_TTL_MS = 60_000;
export const MAX_CACHED_LOGINS = 5000;
export const MIN_LOGIN_LENGTH = 4;
export const BUCKET_CAPACITY = 30;
export const BUCKET_REFILL_PER_SECOND = 0.5;

export type Resolution = { kind: 'found'; viewerId: string } | { kind: 'missing' } | { kind: 'busy' } | { kind: 'error' };

export type Lookup<V> = { kind: 'found'; value: V } | { kind: 'missing' } | { kind: 'busy' } | { kind: 'error' };

type Resolver<T> = ((login: string) => Promise<T>) & { invalidate(logins: readonly string[]): void };

export interface ResolverDeps {
  lookup: (login: string) => Promise<string>;
  now?: () => number;
  maxEntries?: number;
}

export interface CachedResolverDeps<V> {
  lookup: (login: string) => Promise<V | null>;
  foundTtlMs?: number;
  now?: () => number;
  maxEntries?: number;
}

interface Cached<V> {
  value: V | null;
  expiresAt: number;
}

function settled<V>(value: V | null): Lookup<V> {
  return value === null ? { kind: 'missing' } : { kind: 'found', value };
}

export function createCachedResolver<V>({
  lookup,
  foundTtlMs = FOUND_TTL_MS,
  now = Date.now,
  maxEntries = MAX_CACHED_LOGINS
}: CachedResolverDeps<V>): Resolver<Lookup<V>> {
  const cache = new Map<string, Cached<V>>();
  const inflight = new Map<string, Promise<Lookup<V>>>();
  let tokens = BUCKET_CAPACITY;
  let refilledAt = now();

  function takeToken(): boolean {
    const at = now();
    tokens = Math.min(BUCKET_CAPACITY, tokens + ((at - refilledAt) / 1000) * BUCKET_REFILL_PER_SECOND);
    refilledAt = at;
    if (tokens < 1) return false;
    tokens -= 1;
    return true;
  }

  function cached(login: string): Lookup<V> | null {
    const hit = cache.get(login);
    if (!hit) return null;
    if (hit.expiresAt > now()) return settled(hit.value);
    cache.delete(login);
    return null;
  }

  function remember(login: string, value: V | null): void {
    cache.delete(login);
    if (cache.size >= maxEntries) cache.delete(cache.keys().next().value as string);
    cache.set(login, { value, expiresAt: now() + (value === null ? MISSING_TTL_MS : foundTtlMs) });
  }

  async function fetchOnce(login: string, current: () => boolean): Promise<Lookup<V>> {
    try {
      const value = await lookup(login);
      if (current()) remember(login, value);
      return settled(value);
    } catch {
      return { kind: 'error' };
    }
  }

  const resolve = async (login: string): Promise<Lookup<V>> => {
    if (login.length < MIN_LOGIN_LENGTH) return { kind: 'missing' };
    const hit = cached(login);
    if (hit) return hit;
    const pending = inflight.get(login);
    if (pending) return pending;
    if (!takeToken()) return { kind: 'busy' };
    const current = () => inflight.get(login) === started;
    const started = fetchOnce(login, current).finally(() => {
      if (current()) inflight.delete(login);
    });
    inflight.set(login, started);
    return started;
  };

  return Object.assign(resolve, {
    invalidate(logins: readonly string[]): void {
      for (const login of logins) {
        cache.delete(login);
        inflight.delete(login);
      }
    }
  });
}

export function createViewerResolver({ lookup, ...rest }: ResolverDeps): Resolver<Resolution> {
  const resolve = createCachedResolver<string>({ lookup: async (login) => (await lookup(login)) || null, ...rest });
  const viewer = async (login: string): Promise<Resolution> => {
    const res = await resolve(login);
    return res.kind === 'found' ? { kind: 'found', viewerId: res.value } : res;
  };
  return Object.assign(viewer, { invalidate: resolve.invalidate });
}
