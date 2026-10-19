// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const FOUND_TTL_MS = 10 * 60_000;
export const MISSING_TTL_MS = 60_000;
export const MAX_CACHED_LOGINS = 5000;
export const BUCKET_CAPACITY = 30;
export const BUCKET_REFILL_PER_SECOND = 0.5;

export type Resolution = { kind: 'found'; viewerId: string } | { kind: 'missing' } | { kind: 'busy' } | { kind: 'error' };

export interface ResolverDeps {
  lookup: (login: string) => Promise<string>;
  now?: () => number;
  maxEntries?: number;
}

interface Cached {
  viewerId: string;
  expiresAt: number;
}

function settled(viewerId: string): Resolution {
  return viewerId ? { kind: 'found', viewerId } : { kind: 'missing' };
}

export function createViewerResolver({ lookup, now = Date.now, maxEntries = MAX_CACHED_LOGINS }: ResolverDeps): (login: string) => Promise<Resolution> {
  const cache = new Map<string, Cached>();
  const inflight = new Map<string, Promise<Resolution>>();
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

  function cached(login: string): Resolution | null {
    const hit = cache.get(login);
    if (!hit) return null;
    if (hit.expiresAt > now()) return settled(hit.viewerId);
    cache.delete(login);
    return null;
  }

  function remember(login: string, viewerId: string): void {
    cache.delete(login);
    if (cache.size >= maxEntries) cache.delete(cache.keys().next().value as string);
    cache.set(login, { viewerId, expiresAt: now() + (viewerId ? FOUND_TTL_MS : MISSING_TTL_MS) });
  }

  async function fetchOnce(login: string): Promise<Resolution> {
    try {
      const viewerId = await lookup(login);
      remember(login, viewerId);
      return settled(viewerId);
    } catch {
      return { kind: 'error' };
    }
  }

  return async (login) => {
    const hit = cached(login);
    if (hit) return hit;
    const pending = inflight.get(login);
    if (pending) return pending;
    if (!takeToken()) return { kind: 'busy' };
    const started = fetchOnce(login).finally(() => inflight.delete(login));
    inflight.set(login, started);
    return started;
  };
}
