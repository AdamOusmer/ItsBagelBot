import type { SongQueueDoc, SongQueueEntry } from '@bagel/kit/server/songqueue-store';
import type { SpotifyPlayerQueue } from './spotify-store';

export interface QueueView {
  current: QueueRow | null;
  up: QueueRow[];
}

interface QueueRow {
  title: string;
  artists: string;
  requester: string;
}

const row = (entry: SongQueueEntry): QueueRow => ({
  title: entry.title,
  artists: (entry.artists ?? []).join(', '),
  requester: entry.req_name
});

// The dashboard polls while it is visible. Reconcile its rendered list from
// Spotify without writing Valkey from the web process; Sesame owns that CAS.
export function shapeQueue(doc: SongQueueDoc, live: SpotifyPlayerQueue | null, now = Date.now()): QueueView {
  let current = doc.current ?? null;
  let up = [...(doc.up ?? [])];
  const upcoming = live?.up_next?.map((track) => track.id).filter(Boolean) ?? [];
  const currentId = live?.current?.id ?? '';

  if (live && (currentId || upcoming.length)) {
    if (currentId && current?.tid !== currentId) {
      const index = up.findIndex((entry) => entry.tid === currentId);
      current = index >= 0 ? up[index] : null;
      if (index >= 0) up = up.slice(index + 1);
    } else if (!currentId) {
      current = null;
    }

    const available = new Map<string, number>();
    for (const id of upcoming) available.set(id, (available.get(id) ?? 0) + 1);
    const matched = new Set<number>();
    let lastMatch = -1;
    for (let i = up.length - 1; i >= 0; i--) {
      const count = available.get(up[i].tid) ?? 0;
      if (!count) continue;
      available.set(up[i].tid, count - 1);
      matched.add(i);
      if (i > lastMatch) lastMatch = i;
    }
    up = up.filter(
      (entry, i) =>
        matched.has(i) || !(i < lastMatch || (upcoming.length < 20 && entry.at > 0 && entry.at < now - 15_000))
    );
  }

  return {
    current: current ? row(current) : null,
    up: up.slice(0, 10).map(row)
  };
}
