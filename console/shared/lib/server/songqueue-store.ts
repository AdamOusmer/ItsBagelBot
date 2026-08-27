// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Read-only view of sesame's live song-request queue. The console displays
// the queue; chat owns every write.
//
// This module carries its own small read client instead of riding
// valkey-store's pool: that store's private runner is deliberately not
// exported, and the queue doc is sesame's data, not part of the console's
// session/user cache fabric. The connection settings mirror valkey-store's
// (same endpoint helpers, no offline queue, short timeouts) so both degrade
// the same way when Valkey is unreachable.
import Redis from 'iovalkey';
import type { Redis as RedisClient } from 'iovalkey';
import { getServerConfig } from './config';
import { VALKEY_TLS_DATA_PORT, valkeyEndpoint, valkeyTLSOptions } from './valkey-connection';

// SongQueueEntry mirrors the wire shape sesame's ValkeySongQueueStore writes
// (app/sesame/engine/songqueue_valkey.go, songQueueDoc): short JSON keys, one
// doc per broadcaster under songqueue:doc:<id>.
export interface SongQueueEntry {
  tid: string;
  title: string;
  artists?: string[];
  dur: number;
  art?: string;
  url?: string;
  req_id: string;
  req_name: string;
  at: number;
}

export interface SongQueueDoc {
  current?: SongQueueEntry;
  up?: SongQueueEntry[];
}

let client: RedisClient | null = null;
let disabled = false;

function get(): RedisClient | null {
  if (disabled) return null;
  if (client) return client;
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
  client.on('error', () => {});
  return client;
}

/**
 * getSongQueue reads one broadcaster's queue. A miss, an unparseable doc and
 * an unreachable Valkey all read back as the empty queue: this backs a
 * display panel, and "nothing waiting" is the honest degradation for every
 * one of those states. The node-local replica may lag a write by a moment,
 * which a queue display tolerates by construction.
 */
export async function getSongQueue(broadcasterId: string): Promise<SongQueueDoc> {
  const c = get();
  if (!c) return {};
  try {
    const raw = await c.get(`songqueue:doc:${broadcasterId}`);
    if (!raw) return {};
    return shapeDoc(JSON.parse(raw));
  } catch {
    return {};
  }
}

// shapeDoc keeps only what a well-formed doc carries: a corrupt or foreign
// value under the key must degrade to the empty queue, not flow typed-but-
// wrong into the page (a string where an array is expected happily answers
// .slice and then explodes on .map).
function shapeDoc(parsed: unknown): SongQueueDoc {
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
  const d = parsed as { current?: unknown; up?: unknown };
  const doc: SongQueueDoc = {};
  if (d.current && typeof d.current === 'object' && !Array.isArray(d.current)) {
    doc.current = d.current as SongQueueEntry;
  }
  if (Array.isArray(d.up)) {
    doc.up = d.up.filter((e): e is SongQueueEntry => !!e && typeof e === 'object');
  }
  return doc;
}
