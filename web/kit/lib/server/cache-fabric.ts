// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { SwrCache, type CachePolicy, type CacheEvent } from './cache';
import { startInvalidationBus, type ScopeMap } from './invalidation';
import type { CacheKey } from './cache-keys';

export interface CacheMetrics {
  event(event: CacheEvent, key: string): void;
  maskedError(err: unknown, key: string): void;
}

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
  app: string;
  scopes: ScopeMap;
  metrics?: CacheMetrics | null;
  capacity?: number;
  onInvalidation?: (scope: string, id: string) => void;
}

export interface ReadThrough<T> {
  l2?: () => Promise<{ hit: boolean; value: T }>;
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

  readKey<T>(rawKey: string, policy: CachePolicy | number, load: () => Promise<T>): Promise<T> {
    return this.cache.getOrLoad(rawKey, policy, load);
  }

  set<T>(key: CacheKey, id: string, value: T, policy?: CachePolicy | number): void {
    this.cache.set(key.for(id), value, policy ?? key.policy);
  }

  invalidate(...prefixes: string[]): void {
    this.cache.invalidate(...prefixes);
  }

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
