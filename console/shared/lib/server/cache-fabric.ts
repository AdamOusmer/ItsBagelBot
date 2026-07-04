// CacheFabric: one per-app facade over the hybrid read path.
//
//   L1  SwrCache        — in-process, SWR + single-flight + generations.
//   L2  Valkey readers  — node-local projection, only for keys that have one
//                         (tier/account/ban, commands, modules). Admin keys
//                         have no projection and degrade cleanly to L1-only.
//   L3  RPC loader      — the caller-supplied load function.
//
// The fabric also owns the invalidation-bus wiring (scope map as data) and the
// New Relic cache metrics, so apps construct one fabric in their services
// module and stop hand-rolling cached()/invalidate()/listener plumbing.
//
// The console never WRITES the Valkey projection — the Go projector owns it.
// The write half of the hybrid model stays the existing projector `*.replace`
// RPC push after mutations.
import newrelic from 'newrelic';
import { SwrCache, type CachePolicy, type CacheEvent } from './cache';
import { startInvalidationBus, type ScopeMap } from './invalidation';
import type { CacheKey } from './cache-keys';

export interface CacheMetrics {
  event(event: CacheEvent, key: string): void;
  maskedError(err: unknown, key: string): void;
}

/** Default recorder: Custom/Cache/<app>/<key-prefix>/<event> counters, plus a
 *  noticeError for failures masked by stale-serving (otherwise invisible). */
export function newRelicMetrics(app: string): CacheMetrics {
  const family = (key: string) => {
    const i = key.indexOf(':');
    return i > 0 ? key.slice(0, i) : key;
  };
  return {
    event(event, key) {
      newrelic.recordMetric(`Custom/Cache/${app}/${family(key)}/${event}`, 1);
    },
    maskedError(err, key) {
      newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
        component: 'cache-fabric',
        app,
        cacheKey: key,
        maskedByStale: true
      });
    }
  };
}

const NOOP_METRICS: CacheMetrics = {
  event() {},
  maskedError() {}
};

export interface CacheFabricOptions {
  /** 'dashboard' | 'admin' — used in metric names. */
  app: string;
  /** Invalidation-bus routing, declared as data. Must include '*'. */
  scopes: ScopeMap;
  /** Injectable for tests/DEMO. Defaults to New Relic; pass null to disable. */
  metrics?: CacheMetrics | null;
  capacity?: number;
  /** Tap fired for every applied invalidation (scope + broadcaster id) AFTER the
   *  local cache is evicted. The dashboard forwards it to connected browsers over
   *  SSE so an open page re-fetches the instant Go state changes (no polling). */
  onInvalidation?: (scope: string, id: string) => void;
}

export interface ReadThrough<T> {
  /** Node-local Valkey read. `hit: false` (miss sentinel) falls through to load. */
  l2?: () => Promise<{ hit: boolean; value: T }>;
  /** Authoritative loader (RPC). */
  load: () => Promise<T>;
}

export class CacheFabric {
  readonly cache: SwrCache;
  private readonly scopes: ScopeMap;
  private readonly metrics: CacheMetrics;
  private readonly onInvalidation?: (scope: string, id: string) => void;
  private started = false;

  constructor(opts: CacheFabricOptions) {
    this.scopes = opts.scopes;
    this.onInvalidation = opts.onInvalidation;
    this.metrics = opts.metrics === null ? NOOP_METRICS : (opts.metrics ?? newRelicMetrics(opts.app));
    this.cache = new SwrCache({
      capacity: opts.capacity,
      onEvent: (event, key) => this.metrics.event(event, key),
      onMaskedError: (err, key) => this.metrics.maskedError(err, key)
    });
  }

  /**
   * Hybrid read for one key family + id: L1 -> L2 (when the key has a
   * projection) -> load. The composed L2+load runs UNDER the L1 single-flight,
   * so concurrent cold readers still coalesce on one Valkey/RPC round trip.
   */
  read<T>(key: CacheKey, id: string, through: ReadThrough<T>): Promise<T> {
    return this.cache.getOrLoad(key.for(id), key.policy, async () => {
      if (through.l2) {
        const v = await through.l2();
        if (v.hit) {
          this.metrics.event('hit', `l2:${key.prefix}`);
          return v.value;
        }
      }
      return through.load();
    });
  }

  /** L1 read/load without an id-keyed family (e.g. singleton snapshots). */
  readKey<T>(rawKey: string, policy: CachePolicy | number, load: () => Promise<T>): Promise<T> {
    return this.cache.getOrLoad(rawKey, policy, load);
  }

  /** Authoritative overwrite (optimistic write-through). */
  set<T>(key: CacheKey, id: string, value: T, policy?: CachePolicy | number): void {
    this.cache.set(key.for(id), value, policy ?? key.policy);
  }

  invalidate(...prefixes: string[]): void {
    this.cache.invalidate(...prefixes);
  }

  /** Start the invalidation-bus subscription. Call once from the init hook. */
  start(): void {
    if (this.started) return;
    this.started = true;
    startInvalidationBus({
      cache: this.cache,
      scopes: this.scopes,
      onApplied: this.onInvalidation ? (scope, id) => this.onInvalidation!(scope, id) : undefined
    });
  }
}

export function createCacheFabric(opts: CacheFabricOptions): CacheFabric {
  return new CacheFabric(opts);
}
