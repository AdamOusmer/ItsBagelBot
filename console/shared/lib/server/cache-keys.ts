// Typed cache-key registry + policy table.
//
// Every cached read in the consoles belongs to one of a handful of data
// classes; the class decides the freshness windows, and the key prefix decides
// the invalidation-bus routing. Declaring both here (instead of ad hoc string
// keys and TTL constants at each call site) keeps the two apps' caching
// behavior consistent and greppable in one place.
//
// Why these windows:
//   * live       — poll-driven snapshots (shard state). No bus scope covers
//     them; the clock IS the freshness mechanism, so it stays tight.
//   * adminRead / adminPage — operator views. Bounded staleness (≤5s/≤3s
//     fresh) with a short SWR tail so repeat navigation is instant while the
//     background refresh keeps the view honest.
//   * entity     — per-user dashboard state (tier, account, grant,
//     delegations). Push-invalidated on the bus; the fresh window is only a
//     missed-message safety net. Stale-if-error lets a users-service outage
//     degrade to last-known state instead of erroring the page.
//   * security   — the ban gate. Short fresh window, bounded stale-if-error:
//     during an outage the last KNOWN ban state keeps being enforced (an
//     already-banned user stays banned) instead of failing open.
//   * projected  — commands/modules lists. Fully event-invalidated
//     (bagel.cache.invalidate.commands|modules) AND flushed wholesale after
//     any bus connectivity gap, so long windows are safe and cut Valkey/RPC
//     traffic; the bus, not the clock, is the freshness mechanism.

import type { CachePolicy } from './cache';

export type { CachePolicy } from './cache';

export const POLICY = {
  live: { freshMs: 1_000, swrMs: 2_000 },
  adminRead: { freshMs: 5_000, swrMs: 30_000 },
  adminPage: { freshMs: 3_000, swrMs: 30_000 },
  entity: { freshMs: 120_000, swrMs: 600_000, staleIfErrorMs: 600_000 },
  security: { freshMs: 60_000, swrMs: 120_000, staleIfErrorMs: 300_000 },
  projected: { freshMs: 600_000, swrMs: 1_800_000 },
  // govee: the third-party device list + key-presence flag. Both are read on
  // every govee page load (and every /events invalidation), but the underlying
  // Govee cloud call is slow and rate-limited, so a short fresh window plus a
  // long SWR/stale-if-error tail renders the picker instantly and refreshes in
  // the background. Not bus-scoped: freshness comes from the clock plus an
  // explicit flush on key set/clear (see govee-store).
  govee: { freshMs: 60_000, swrMs: 600_000, staleIfErrorMs: 600_000 }
} as const satisfies Record<string, CachePolicy>;

export type PolicyName = keyof typeof POLICY;

/** A registered key family: a stable prefix bound to one policy. */
export interface CacheKey {
  readonly prefix: string;
  readonly policy: CachePolicy;
  /** Build the concrete key for one id, e.g. keys.commands.for('123'). */
  for(id: string): string;
}

/**
 * Declare a key family. The prefix must be unique per app cache; invalidation
 * routes by `prefix:` so families are evicted wholesale by prefix match.
 */
export function defineKey(prefix: string, policy: CachePolicy): CacheKey {
  return {
    prefix,
    policy,
    for: (id: string) => `${prefix}:${id}`
  };
}
