// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { masterClient } from './valkey-master';

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

export async function getSongQueue(broadcasterId: string): Promise<SongQueueDoc> {
  const c = masterClient();
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
