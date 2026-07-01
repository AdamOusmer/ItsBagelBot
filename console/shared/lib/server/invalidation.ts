// Cache-invalidation bus consumer: durable transport + data-driven key routing.
//
// Go services publish `${prefix}.<scope>` with a `{ broadcaster_id }` body when
// they mutate user state. Every console replica subscribes (no queue group:
// each owns its own in-process cache and must hear every message) and evicts
// the affected keys immediately, so writes propagate without waiting on TTL.
//
// Routing is declared per app as data (a ScopeMap), not as divergent switch
// statements: the subject's last dot-segment picks the scope, the map yields
// the key prefixes to evict. Unknown scopes fall through to the '*' entry so a
// new publisher-side scope degrades to a coarse per-user flush instead of being
// silently ignored.
//
// Transport is subscribeDurable: the subscription retries forever (a boot-time
// NATS outage no longer kills invalidation for the process lifetime) and any
// connectivity gap flushes the whole cache — long-TTL entries must never
// outlive missed invalidations.
import { subscribeDurable } from './nats';
import { getServerConfig } from './config';
import type { SwrCache } from './cache';

/**
 * scope -> key prefixes to evict for one broadcaster id. Must include a '*'
 * fallback for missing/unknown scopes. Returned strings are PREFIXES (evicted
 * via cache.invalidate), so `delegations:<id>` also drops `delegations:<id>:given`.
 */
export type ScopeMap = Record<string, (id: string) => string[]>;

export interface InvalidationBusOptions {
  cache: SwrCache;
  scopes: ScopeMap;
  /** Extra tap per applied invalidation (metrics/logging). */
  onApplied?: (scope: string, id: string, prefixes: string[]) => void;
}

/**
 * Subscribe to the invalidation bus and evict per the scope map. Call once at
 * boot (from the init hook, after registerServerConfig).
 */
export function startInvalidationBus(opts: InvalidationBusOptions): void {
  const prefix = getServerConfig().cacheInvalidationPrefix;
  const fallback = opts.scopes['*'];
  if (!fallback) throw new Error("invalidation scope map must declare a '*' fallback");

  subscribeDurable(
    prefix + '.>',
    (subject, data) => {
      try {
        const msg = JSON.parse(new TextDecoder().decode(data)) as { broadcaster_id?: unknown };
        const id = typeof msg.broadcaster_id === 'string' ? msg.broadcaster_id : undefined;
        if (!id) return;
        // Scope = last dot-segment of the matched subject, e.g.
        // "bagel.cache.invalidate.status" -> "status".
        const scope = subject.slice(subject.lastIndexOf('.') + 1);
        const route = opts.scopes[scope] ?? fallback;
        const prefixes = route(id);
        if (prefixes.length) opts.cache.invalidate(...prefixes);
        opts.onApplied?.(scope, id, prefixes);
      } catch {
        // Malformed message — ignore.
      }
    },
    () => {
      // Connectivity gap: invalidations may have been missed while offline.
      // Flush everything; the SWR windows repopulate on the next reads.
      opts.cache.clear();
    }
  );
}
