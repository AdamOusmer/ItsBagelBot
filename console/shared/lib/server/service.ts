// Declarative RPC service definitions.
//
// Both consoles' service layers are the same shape repeated ~25 times: an RPC
// subject, a request payload built from typed args, a reply mapping, a timeout,
// and (for reads) a cache key + policy with an optional Valkey L2 reader.
// defineRead/defineWrite capture that shape once, so each app's services module
// shrinks to a table of declarations and the transport/caching behavior cannot
// drift between apps.
//
// Wire behavior is delegated to the existing primitives unchanged: rpc() still
// throws RpcError on transport failure / missing responder / an `error` reply
// field; the fabric still owns single-flight, SWR, and invalidation.
import { rpc } from './nats';
import type { CacheFabric } from './cache-fabric';
import type { CachePolicy } from './cache';

// Cached reads are primary-key lookups that return in low ms when healthy; 2s
// caps a slow/missing responder so SSR degrades fast instead of hanging to the
// 5s default. Writes keep the 5s default to match the Go callers.
export const READ_TIMEOUT_MS = 2000;
export const WRITE_TIMEOUT_MS = 5000;

export interface ReadCacheSpec<A extends unknown[], T> {
  fabric: CacheFabric;
  key: (...args: A) => string;
  policy: CachePolicy | number;
  /** Node-local Valkey read; `hit: false` (miss sentinel) falls through to RPC.
   *  Runs UNDER the L1 single-flight, so cold readers still coalesce. */
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
  /** Defaults to identity (the raw reply). */
  map?: (reply: W) => T;
  timeoutMs?: number;
  /** Cache upkeep after a successful write: invalidate and/or write-through.
   *  Runs synchronously before the result is returned. */
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
