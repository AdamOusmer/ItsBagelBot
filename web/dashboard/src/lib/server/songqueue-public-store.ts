// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match app/twitch/sesame/engine/songqueue_valkey.go's document: read-only here, and from the replica so public traffic never loads the primary.
import { replicaClient } from '@bagel/kit/server/valkey-store';
import { decodeSongQueueDoc, songQueueKey, type SongQueueDoc, type SongQueueEntry } from '@bagel/kit/server/songqueue-store';
import { valkeyGuard } from './valkey-guard';
import { degradedSongQueue, MAX_SONG_ROWS, type SongQueuePublicView, type SongRow } from '../songqueue-snapshot';

const guarded = valkeyGuard('valkey-songqueue-public', 'songqueue-public');

const publicRow = (entry: SongQueueEntry, now: number): SongRow => ({
  title: entry.title,
  artists: (entry.artists ?? []).join(', '),
  requester: entry.req_name,
  joinedAt: entry.at > 0 ? entry.at : now
});

export function publicSongQueue(doc: SongQueueDoc, now = Date.now()): SongQueuePublicView {
  const up = doc.up ?? [];
  return {
    open: true,
    current: doc.current ? publicRow(doc.current, now) : null,
    up: up.slice(0, MAX_SONG_ROWS).map((entry) => publicRow(entry, now)),
    total: up.length,
    degraded: false
  };
}

export async function readPublicSongQueue(uid: string): Promise<SongQueuePublicView> {
  const c = replicaClient();
  if (!c) return degradedSongQueue();
  try {
    return publicSongQueue(decodeSongQueueDoc(await guarded(() => c.get(songQueueKey(uid)))));
  } catch {
    return degradedSongQueue();
  }
}
