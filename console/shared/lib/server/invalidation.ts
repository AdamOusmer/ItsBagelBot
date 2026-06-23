// Cache-invalidation bus consumer (transport only).
//
// Go services publish `${prefix}.<scope>` with a `{ broadcaster_id }` body when
// they mutate user state. Every console replica subscribes (no queue group:
// each owns its own in-process cache and must hear every message) and evicts the
// affected keys immediately, so writes propagate without waiting on TTL.
//
// This module owns only the transport + message parsing; key-routing (which
// scope evicts which cache key) is the app's concern, passed in as the handler.
// That keeps the dashboard and admin scope maps separate while sharing the wire.
import { subscribe } from './nats';
import { getServerConfig } from './config';

/** Called per invalidation message with the parsed broadcaster id and scope. */
export type InvalidationHandler = (broadcasterId: string, scope: string | undefined) => void;

/**
 * Subscribe to the invalidation bus and dispatch to `onInvalidate`. Call once at
 * boot (from the init hook, after registerServerConfig). Fire-and-forget and
 * resilient to NATS restarts via the shared subscribe() primitive.
 */
export function startInvalidationBus(onInvalidate: InvalidationHandler): void {
  const prefix = getServerConfig().cacheInvalidationPrefix;
  subscribe(prefix + '.>', (subject, data) => {
    try {
      const msg = JSON.parse(new TextDecoder().decode(data)) as { broadcaster_id?: unknown };
      const id = typeof msg.broadcaster_id === 'string' ? msg.broadcaster_id : undefined;
      if (!id) return;
      // Scope = last dot-segment of the matched subject, e.g.
      // "bagel.cache.invalidate.status" -> "status".
      const scope = subject.slice(subject.lastIndexOf('.') + 1) || undefined;
      onInvalidate(id, scope);
    } catch {
      // Malformed message — ignore.
    }
  });
}
