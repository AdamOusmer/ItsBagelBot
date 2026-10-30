// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { subscribeDurable } from './nats';
import { getServerConfig } from './config';
import type { SwrCache } from './cache';

// `keys` is the optional extra list a publisher attaches (e.g. rename logins); empty when absent.
export type ScopeMap = Record<string, (id: string, keys: readonly string[]) => string[]>;

const MAX_KEYS = 8;
const MAX_KEY_LENGTH = 64;

export interface InvalidationBusOptions {
  cache: SwrCache;
  scopes: ScopeMap;
  onApplied?: (scope: string, id: string, prefixes: string[], keys: readonly string[]) => void;
}

const decoder = new TextDecoder();

function parseKeys(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  return raw.filter((k): k is string => typeof k === 'string' && k.length > 0 && k.length <= MAX_KEY_LENGTH).slice(0, MAX_KEYS);
}

export function startInvalidationBus(opts: InvalidationBusOptions): void {
  const prefix = getServerConfig().cacheInvalidationPrefix;
  const fallback = opts.scopes['*'];
  if (!fallback) throw new Error("invalidation scope map must declare a '*' fallback");

  subscribeDurable(
    prefix + '.>',
    (subject, data) => {
      try {
        const msg = JSON.parse(decoder.decode(data)) as { broadcaster_id?: unknown; keys?: unknown };
        const id = typeof msg.broadcaster_id === 'string' ? msg.broadcaster_id : undefined;
        if (!id) return;
        const scope = subject.slice(subject.lastIndexOf('.') + 1);
        const keys = parseKeys(msg.keys);
        const route = opts.scopes[scope] ?? fallback;
        const prefixes = route(id, keys);
        if (prefixes.length) opts.cache.invalidate(...prefixes);
        opts.onApplied?.(scope, id, prefixes, keys);
      } catch {}
    },
    () => {
      opts.cache.clear();
    }
  );
}
