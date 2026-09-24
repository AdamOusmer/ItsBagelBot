// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribeDurable } from './nats';
import { getServerConfig } from './config';
import type { SwrCache } from './cache';

export type ScopeMap = Record<string, (id: string) => string[]>;

export interface InvalidationBusOptions {
  cache: SwrCache;
  scopes: ScopeMap;
  onApplied?: (scope: string, id: string, prefixes: string[]) => void;
}

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
        const scope = subject.slice(subject.lastIndexOf('.') + 1);
        const route = opts.scopes[scope] ?? fallback;
        const prefixes = route(id);
        if (prefixes.length) opts.cache.invalidate(...prefixes);
        opts.onApplied?.(scope, id, prefixes);
      } catch {}
    },
    () => {
      opts.cache.clear();
    }
  );
}
