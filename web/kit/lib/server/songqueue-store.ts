// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import Redis from 'iovalkey';
import type { Redis as RedisClient } from 'iovalkey';
import { getServerConfig } from './config';
import { VALKEY_TLS_DATA_PORT, valkeyEndpoint, valkeyTLSOptions } from './valkey-connection';

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

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function shapeDoc(parsed: unknown): SongQueueDoc {
  if (!isRecord(parsed)) return {};
  const doc: SongQueueDoc = {};
  if (isRecord(parsed.current)) doc.current = parsed.current as unknown as SongQueueEntry;
  if (Array.isArray(parsed.up)) {
    doc.up = parsed.up.filter((e): e is SongQueueEntry => isRecord(e));
  }
  return doc;
}
