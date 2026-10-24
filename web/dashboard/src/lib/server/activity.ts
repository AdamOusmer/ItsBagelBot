// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match internal/activity/store.go's key layout and wire format.
import type Redis from 'iovalkey';
import { replicaClient } from '@bagel/kit/server/valkey-store';
import { valkeyGuard } from './valkey-guard';
import {
  degradedActivityFeed,
  type ActivityFeed,
  type ActivityKind,
  type ActivityRow
} from '../overview-live';

const FEED_PREFIX = 'activity:feed:';
const LATENCY_PREFIX = 'activity:latency:';
const DROPPED_PREFIX = 'activity:dropped:';

const FEED_CAP = 50;
const LATENCY_CAP = 32;

const guarded = valkeyGuard('valkey-activity-feed', 'activity-feed');

interface wireRow {
  k?: ActivityKind;
  x?: string;
  m?: string;
  a?: string;
  d?: number;
}

function decodeRow(raw: string, index: number): ActivityRow | null {
  let w: wireRow;
  try {
    w = JSON.parse(raw) as wireRow;
  } catch {
    return null;
  }
  if (!w.k || !w.a) return null;
  return { id: `${w.a}-${index}`, kind: w.k, text: w.x ?? '', meta: w.m ?? '' as string, at: w.a };
}

function decodeRows(raw: string[]): ActivityRow[] {
  const rows: ActivityRow[] = [];
  raw.forEach((r, i) => {
    const row = decodeRow(r, i);
    if (row) rows.push(row);
  });
  return rows;
}

function median(raw: string[]): number | null {
  const vals = raw.map((r) => Number.parseInt(r, 10)).filter((n) => Number.isFinite(n));
  if (vals.length === 0) return null;
  vals.sort((a, b) => a - b);
  return vals[Math.floor(vals.length / 2)];
}

function parseDropped(raw: string | null): number {
  if (!raw) return 0;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) ? n : 0;
}

async function readFeed(c: Redis, uid: string): Promise<ActivityFeed> {
  const [rowsRaw, latencyRaw, droppedRaw] = await Promise.all([
    c.lrange(FEED_PREFIX + uid, 0, FEED_CAP - 1),
    c.lrange(LATENCY_PREFIX + uid, 0, LATENCY_CAP - 1),
    c.get(DROPPED_PREFIX + uid)
  ]);
  return {
    rows: decodeRows(rowsRaw),
    medianMs: median(latencyRaw),
    dropped: parseDropped(droppedRaw),
    ok: true
  };
}

export async function activityFeed(uid: string): Promise<ActivityFeed> {
  const c = replicaClient();
  if (!c) return degradedActivityFeed();
  try {
    return await guarded(() => readFeed(c, uid));
  } catch {
    return degradedActivityFeed();
  }
}
