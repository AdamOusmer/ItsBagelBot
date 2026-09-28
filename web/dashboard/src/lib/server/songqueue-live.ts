// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { getSongQueue, type SongQueueDoc } from '@bagel/kit/server/songqueue-store';
import { readSpotifyPlayerQueue, spotifyStore } from './spotify-store';
import { shapeQueue, type QueueView } from './songqueue-view';

function hasStoredSongs(queue: SongQueueDoc): boolean {
  if (queue.current) return true;
  return (queue.up?.length ?? 0) > 0;
}

export async function queueView(uid: string, connected: boolean, queue: SongQueueDoc): Promise<QueueView> {
  if (!connected || !hasStoredSongs(queue)) return shapeQueue(queue, null);
  return shapeQueue(queue, await readSpotifyPlayerQueue(uid));
}

export async function readQueueView(uid: string): Promise<QueueView> {
  const [grant, queue] = await Promise.all([spotifyStore(uid).grant(), getSongQueue(uid)]);
  return queueView(uid, grant.connected, queue);
}
