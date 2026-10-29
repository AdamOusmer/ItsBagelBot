// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match app/twitch/sesame/engine/queue_valkey.go's key layout: read-only here.
import type Redis from 'iovalkey';
import { replicaClient } from '@bagel/kit/server/valkey-store';
import { valkeyGuard } from './valkey-guard';
import { degradedQueue, MAX_QUEUE_ENTRIES, type QueueEntry, type QueueView } from '../queue-snapshot';

const LINE_PREFIX = 'queue:line:';
const OPEN_PREFIX = 'queue:open:';

const guarded = valkeyGuard('valkey-queue-line', 'queue-line');

function decodeEntries(flat: string[]): QueueEntry[] {
  const entries: QueueEntry[] = [];
  for (let i = 0; i + 1 < flat.length; i += 2) {
    const joinedAt = Number(flat[i + 1]);
    if (flat[i] && Number.isFinite(joinedAt)) entries.push({ login: flat[i], joinedAt });
  }
  return entries;
}

async function readLine(c: Redis, uid: string): Promise<QueueView> {
  const line = LINE_PREFIX + uid;
  const replies = await c.pipeline().exists(OPEN_PREFIX + uid).zcard(line).zrange(line, 0, MAX_QUEUE_ENTRIES - 1, 'WITHSCORES').exec();
  if (!replies) throw new Error('queue pipeline aborted');
  for (const [err] of replies) if (err) throw err;
  const [open, total, flat] = replies.map(([, value]) => value);
  return { open: open === 1, total: Number(total), entries: decodeEntries(flat as string[]), degraded: false };
}

export async function readQueue(uid: string): Promise<QueueView> {
  const c = replicaClient();
  if (!c) return degradedQueue();
  try {
    return await guarded(() => readLine(c, uid));
  } catch {
    return degradedQueue();
  }
}
