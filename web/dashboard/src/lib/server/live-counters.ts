// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { masterClient } from '@bagel/kit/server/valkey-master';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';

// internal/projection/valkey_live.go writes these keys; change both together.
const LIVE_PREFIX = 'ctr:live:';
const BOARD_PREFIX = 'ctr:board:';
const BOARD_SEEDED_PREFIX = 'ctr:board-seeded:';
const SEEDED_FIELD = 'seeded_at';

const OP_TIMEOUT_MS = 200;

const breaker = new CircuitBreaker({ name: 'valkey-live-counters', failureThreshold: 3, resetMs: 5_000 });

export type LiveTotals = Record<string, number>;

export function parseTotals(names: readonly string[], reply: (string | null)[]): LiveTotals | null {
  const [seeded, ...values] = reply;
  if (seeded === null || seeded === undefined) return null;
  return Object.fromEntries(names.map((name, i) => [name, Number(values[i]) || 0]));
}

export function parseBoard(seeded: number, flat: string[]): Map<string, number> | null {
  if (!seeded) return null;
  const ranked = new Map<string, number>();
  for (let i = 0; i + 1 < flat.length; i += 2) ranked.set(flat[i], Number(flat[i + 1]) || 0);
  return ranked;
}

function guarded<T>(op: () => Promise<T>): Promise<T> {
  return breaker.run(() => withTimeout(op(), OP_TIMEOUT_MS, 'valkey-live-counters'));
}

export async function liveTotals(userId: string, names: readonly string[]): Promise<LiveTotals | null> {
  const c = masterClient();
  if (!c) return null;
  try {
    return parseTotals(names, await guarded(() => c.hmget(LIVE_PREFIX + userId, SEEDED_FIELD, ...names)));
  } catch {
    return null;
  }
}

export async function liveBoard(name: string, limit: number): Promise<Map<string, number> | null> {
  const c = masterClient();
  if (!c) return null;
  try {
    const [seeded, flat] = await guarded(() =>
      Promise.all([c.exists(BOARD_SEEDED_PREFIX + name), c.zrevrange(BOARD_PREFIX + name, 0, limit - 1, 'WITHSCORES')])
    );
    return parseBoard(seeded, flat);
  } catch {
    return null;
  }
}
