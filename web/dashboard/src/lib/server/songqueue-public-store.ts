// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Must match app/twitch/sesame/engine/songqueue_valkey.go's document: read-only here, and from the replica so public traffic never loads the primary.
import { replicaClient } from '@bagel/kit/server/valkey-store';
import { decodeSongQueueDoc, songQueueKey, type SongQueueDoc } from '@bagel/kit/server/songqueue-store';
import { valkeyGuard } from './valkey-guard';
import { shapeQueue, type QueueRow } from './songqueue-view';
import { degradedSongQueue, type SongQueuePublicView, type SongRow } from '../songqueue-snapshot';

const guarded = valkeyGuard('valkey-songqueue-public', 'songqueue-public');

const publicRow = ({ title, artists, requester }: QueueRow): SongRow => ({ title, artists, requester });

export function publicSongQueue(doc: SongQueueDoc): SongQueuePublicView {
  const { current, up } = shapeQueue(doc, null);
  return { current: current && publicRow(current), up: up.map(publicRow), total: doc.up?.length ?? 0, degraded: false };
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
