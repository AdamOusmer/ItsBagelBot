// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match internal/chatvolume's wire format.
import { masterClient } from '@bagel/kit/server/valkey-master';
import { CircuitBreaker, withTimeout } from '@bagel/kit/server/resilience';
import { logger } from '@bagel/kit/server/logger';
import { allRead, finiteOrNull, degradedChatVolume, type ChatVolume } from '../overview-live';

const RING_WIDTH = 60;
const OP_TIMEOUT_MS = 200;
const KEY_PREFIX = 'chatvol:';

const breaker = new CircuitBreaker({ name: 'valkey-chat-volume', failureThreshold: 3, resetMs: 5_000 });

function chatVolKey(uid: string): string {
  return KEY_PREFIX + uid;
}

function parseSlot(raw: string | undefined, wantDelta: number): { count: number; handled: boolean } {
  const empty = { count: 0, handled: false };
  const parts = raw?.split(':') ?? [];
  if (parts.length !== 3) return empty;

  const nums = [finiteOrNull(parts[0]), finiteOrNull(parts[1])];
  if (!allRead(nums)) return empty;

  const [delta, count] = nums;
  return delta === wantDelta ? { count, handled: parts[2] === '1' } : empty;
}

function slotName(epoch: number): string {
  return String(((epoch % RING_WIDTH) + RING_WIDTH) % RING_WIDTH);
}

function buildChatVolume(fields: Record<string, string>, nowEpochMin: number): ChatVolume {
  const anchor = Number(fields.a);
  const buckets: number[] = [];
  const commandTicks: number[] = [];
  for (let i = 0; i < RING_WIDTH; i++) {
    const target = nowEpochMin - (RING_WIDTH - 1) + i;
    const { count, handled } = parseSlot(fields[slotName(target)], target - anchor);
    buckets.push(count);
    if (handled) commandTicks.push(i);
  }
  return {
    buckets,
    commandTicks,
    now: buckets[RING_WIDTH - 1] ?? 0,
    peak: buckets.reduce((max, v) => Math.max(max, v), 0),
    ok: true
  };
}

export async function chatVolume(uid: string): Promise<ChatVolume> {
  const c = masterClient();
  if (!c) return degradedChatVolume();
  try {
    const fields = await breaker.run(() =>
      withTimeout(c.hgetall(chatVolKey(uid)), OP_TIMEOUT_MS, 'valkey chat-volume read')
    );
    return buildChatVolume(fields, Math.floor(Date.now() / 60_000));
  } catch (err) {
    logger.warn({ err }, '[chat-volume] read failed, degrading');
    return degradedChatVolume();
  }
}
