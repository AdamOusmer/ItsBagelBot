// In-process fan-out of cache-invalidation events to connected browser clients.
//
// The Go cache-invalidation bus already fans every write out to EVERY console
// replica (no queue group — see shared/lib/server/invalidation.ts). This hub is
// the last hop: it forwards the invalidations for a given board id to that
// board's open SSE connections ON THIS REPLICA, so the browser re-fetches the
// moment state changes instead of polling. A browser is connected to exactly one
// replica at a time, and that replica receives every invalidation, so whichever
// replica holds the SSE connection can always push.
//
// Keyed by the effective board id (delegate_of ?? user_id): owner and delegate
// both watch the owner's board.

type Listener = (scope: string) => void;

const listeners = new Map<string, Set<Listener>>();

/** Register a browser SSE connection's listener for a board id. Returns an
 *  idempotent unsubscribe. */
export function subscribe(boardId: string, fn: Listener): () => void {
  let set = listeners.get(boardId);
  if (!set) {
    set = new Set();
    listeners.set(boardId, set);
  }
  set.add(fn);
  return () => {
    const s = listeners.get(boardId);
    if (!s) return;
    s.delete(fn);
    if (s.size === 0) listeners.delete(boardId);
  };
}

/** Notify a board's connected browsers of one invalidation scope. Fed from the
 *  cache fabric's onInvalidation tap. A no-op when nobody is watching. */
export function publish(boardId: string, scope: string): void {
  const set = listeners.get(boardId);
  if (!set) return;
  for (const fn of set) {
    // One slow/broken listener must never wedge the invalidation bus.
    try {
      fn(scope);
    } catch {
      /* ignore */
    }
  }
}
