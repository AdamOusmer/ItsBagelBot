// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from './nats';
import type { CacheFabric } from './cache-fabric';
import type { CachePolicy } from './cache';

export const READ_TIMEOUT_MS = 2000;
export const WRITE_TIMEOUT_MS = 5000;

export interface ReadCacheSpec<A extends unknown[], T> {
  fabric: CacheFabric;
  key: (...args: A) => string;
  policy: CachePolicy | number;
  l2?: (...args: A) => Promise<{ hit: boolean; value: T }>;
}

export interface ReadDef<A extends unknown[], W, T> {
  subject: string;
  request: (...args: A) => unknown;
  map: (reply: W) => T;
  timeoutMs?: number;
  cache?: ReadCacheSpec<A, T>;
}

export function defineRead<A extends unknown[], W, T>(def: ReadDef<A, W, T>): (...args: A) => Promise<T> {
  const timeout = def.timeoutMs ?? READ_TIMEOUT_MS;
  return (...args: A): Promise<T> => {
    const load = async () => def.map(await rpc<W>(def.subject, def.request(...args), timeout));
    const c = def.cache;
    if (!c) return load();
    return c.fabric.readKey(c.key(...args), c.policy, async () => {
      if (c.l2) {
        const v = await c.l2(...args);
        if (v.hit) return v.value;
      }
      return load();
    });
  };
}

export interface WriteDef<A extends unknown[], W, T = W> {
  subject: string;
  request: (...args: A) => unknown;
  map?: (reply: W) => T;
  timeoutMs?: number;
  after?: (result: T, ...args: A) => void;
}

export function defineWrite<A extends unknown[], W, T = W>(def: WriteDef<A, W, T>): (...args: A) => Promise<T> {
  const timeout = def.timeoutMs ?? WRITE_TIMEOUT_MS;
  return async (...args: A): Promise<T> => {
    const reply = await rpc<W>(def.subject, def.request(...args), timeout);
    const result = def.map ? def.map(reply) : (reply as unknown as T);
    def.after?.(result, ...args);
    return result;
  };
}
