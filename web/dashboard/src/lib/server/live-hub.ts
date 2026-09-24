// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Listener = (scope: string) => void;

const listeners = new Map<string, Set<Listener>>();

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

export function publish(boardId: string, scope: string): void {
  const set = listeners.get(boardId);
  if (!set) return;
  for (const fn of set) {
    // One broken listener must never wedge the invalidation bus.
    try {
      fn(scope);
    } catch {}
  }
}
