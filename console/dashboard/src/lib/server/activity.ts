// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Overview "What the bot just did" feed: a read-only Valkey view. The only
// writer is internal/activity/store.go (Go, sesame + outgress); this file
// mirrors its exact key layout and wire format rather than talking to it
// over RPC, the same direct-read pattern @bagel/shared/server/valkey-store
// uses for the settings projection. Three keys per broadcaster:
//
//   activity:feed:<uid>     LIST, LPUSHed newest-first, JSON per row, capped
//                           to FEED_CAP and TTL'd (see store.go)
//   activity:latency:<uid>  LIST, the last LATENCY_CAP command DurationMS
//                           samples — the input to the median below
//   activity:dropped:<uid>  STRING, store.go's own write-failure counter,
//                           refreshed on every successful write (so it can go
//                           briefly stale during an outage, never wrong-high)
//
// Read consistency: this is a NODE-LOCAL (replica) read, unlike store.go's
// write side, which deliberately uses Primary. That asymmetry is intentional,
// not an oversight: store.go's writer reads its own writes back on the same
// hot pipeline, where the replication window would routinely hide a row that
// was just written. A dashboard page render happens well after the write —
// typically seconds to minutes later — so the same window (milliseconds) is
// not observable here. This matches every other read in valkey-store.ts.
import Redis from 'iovalkey';
import { getServerConfig, hasServerConfig } from '@bagel/shared/server/config';
import { CircuitBreaker, withTimeout } from '@bagel/shared/server/resilience';
import {
  VALKEY_TLS_DATA_PORT,
  valkeyEndpoint,
  valkeyTLSOptions
} from '@bagel/shared/server/valkey-connection';
import {
  degradedActivityFeed,
  type ActivityFeed,
  type ActivityKind,
  type ActivityRow
} from '../overview-live';

const FEED_PREFIX = 'activity:feed:';
const LATENCY_PREFIX = 'activity:latency:';
const DROPPED_PREFIX = 'activity:dropped:';

// Mirrors internal/activity/store.go's feedCap/latencyCap exactly: reading
// fewer than the writer keeps is silently-correct (just older rows), but
// reading MORE would returns nothing extra since Ltrim already bounds the
// list — these are read-side ceilings, not a second cap.
const FEED_CAP = 50;
const LATENCY_CAP = 32;

const OP_TIMEOUT_MS = 200;

// One breaker for this read tier, isolated from valkey-store.ts's own (a
// slow/broken activity read must never trip the settings breaker or vice
// versa) — same per-dependency isolation valkey-master.ts's doc argues for.
const breaker = new CircuitBreaker({ name: 'valkey-activity-feed', failureThreshold: 3, resetMs: 5_000 });

let client: Redis | null = null;
let disabled = false;

/** Lazily build the node-local read client, or null when Valkey is unconfigured. */
function getClient(): Redis | null {
  if (disabled) return null;
  if (client) return client;
  if (!hasServerConfig()) return null;
  const cfg = getServerConfig().valkey;
  if (!cfg) {
    disabled = true;
    return null;
  }
  const tls = valkeyTLSOptions(cfg);
  const endpoint = valkeyEndpoint(cfg.addr, Boolean(tls), VALKEY_TLS_DATA_PORT);
  client = new Redis({
    host: endpoint.host,
    port: endpoint.port,
    password: cfg.password || undefined,
    tls,
    enableOfflineQueue: false,
    maxRetriesPerRequest: 1,
    connectTimeout: 1000,
    retryStrategy: (times) => Math.min(times * 200, 2000)
  });
  // Swallow connection errors; reads degrade below and the client reconnects.
  client.on('error', () => {});
  return client;
}

// wireRow is store.go's on-wire JSON shape (single-letter keys — see that
// file's budget comment for why). Absent fields decode to zero values, which
// decodeRow below treats as "not a usable row" via the k/a presence check.
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
  // LPUSH order already makes the list newest-first; the index just needs to
  // be unique per render, not stable across renders.
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

/**
 * Median of at most LATENCY_CAP recent command durations — an approximate,
 * windowed statistic, not a true median over every command ever answered
 * this stream. See ActivityFeed.medianMs's doc (overview-live.ts) and
 * store.go's matching `median` function, which this mirrors exactly so the
 * number does not depend on which side happened to compute it.
 */
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

/**
 * Reads the Overview activity feed for uid. Never throws: an unconfigured
 * store, an open circuit, a timeout, or any other failure all degrade to
 * {@link degradedActivityFeed}, matching every other lane on this page (see
 * $lib/overview-live's honesty-rule doc — a failed read must not render as a
 * confident empty feed).
 */
export async function activityFeed(uid: string): Promise<ActivityFeed> {
  const c = getClient();
  if (!c) return degradedActivityFeed();
  try {
    return await breaker.run(() => withTimeout(readFeed(c, uid), OP_TIMEOUT_MS, 'activity-feed'));
  } catch {
    return degradedActivityFeed();
  }
}
