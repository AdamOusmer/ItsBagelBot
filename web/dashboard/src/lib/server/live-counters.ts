// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { masterClient } from '@bagel/kit/server/valkey-master';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';
import { parseCounterValue } from '@bagel/kit/validation';

// internal/projection/valkey_live.go writes these keys; change both together.
const LIVE_PREFIX = 'ctr:live:';
const BOARD_PREFIX = 'ctr:board:v2:';
const BOARD_SEEDED_PREFIX = 'ctr:board-seeded:v2:';
const SEEDED_FIELD = 'seeded_at';

const OP_TIMEOUT_MS = 200;

const breaker = new CircuitBreaker({ name: 'valkey-live-counters', failureThreshold: 3, resetMs: 5_000 });

export type LiveTotals = Record<string, string>;

export function parseTotals(names: readonly string[], reply: (string | null)[]): LiveTotals | null {
  const [seeded, ...values] = reply;
  if (seeded === null || seeded === undefined) return null;
  const totals: LiveTotals = {};
  for (let i = 0; i < names.length; i++) {
    const value = parseCounterValue(values[i] ?? '0');
    if (value === null) return null;
    totals[names[i]] = value;
  }
  return totals;
}

export function parseBoard(seeded: number, members: string[]): Map<string, string> | null {
  if (!seeded) return null;
  const ranked = new Map<string, string>();
  for (const member of members) {
    if (!/^\d{19}:[1-9]\d*$/.test(member)) return null;
    const count = parseCounterValue(member.slice(0, 19));
    if (count === null) return null;
    ranked.set(member.slice(20), count);
  }
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

export async function liveBoard(name: string, limit: number): Promise<Map<string, string> | null> {
  const c = masterClient();
  if (!c) return null;
  try {
    const [seeded, members] = await guarded(() =>
      Promise.all([c.exists(BOARD_SEEDED_PREFIX + name), c.zrevrange(BOARD_PREFIX + name, 0, limit - 1)])
    );
    return parseBoard(seeded, members);
  } catch {
    return null;
  }
}
