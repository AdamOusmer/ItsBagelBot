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
  const initial = { current: doc.current ?? null, up: [...(doc.up ?? [])] };
  const player = playerItems(live);
  const visible = player ? reconcileView(initial, player.currentId, player.upcoming, now) : initial;
  return renderQueue(visible);
}

function playerItems(live: SpotifyPlayerQueue | null) {
  if (!live) return null;
  const upcoming = live.up_next?.map((track) => track.id).filter(Boolean) ?? [];
  const currentId = live.current?.id ?? '';
  if (!currentId && upcoming.length === 0) return null;
  return { currentId, upcoming };
}

function renderQueue(visible: QueueEntries): QueueView {
  return {
    current: visible.current ? row(visible.current) : null,
    up: visible.up.slice(0, 10).map(row)
  };
}

type QueueEntries = { current: SongQueueEntry | null; up: SongQueueEntry[] };

function reconcileView(entries: QueueEntries, currentId: string, upcoming: string[], now: number): QueueEntries {
  const playing = alignCurrent(entries, currentId);
  return { current: playing.current, up: filterUpcoming(playing.up, upcoming, now) };
}

function alignCurrent(entries: QueueEntries, currentId: string): QueueEntries {
  if (!currentId) return { current: null, up: entries.up };
  if (entries.current?.tid === currentId) return entries;
  const index = entries.up.findIndex((entry) => entry.tid === currentId);
  if (index < 0) return { current: null, up: entries.up };
  return { current: entries.up[index], up: entries.up.slice(index + 1) };
}

function matchUpcoming(up: SongQueueEntry[], upcoming: string[]): Set<number> {
  const available = new Map<string, number>();
  for (const id of upcoming) available.set(id, (available.get(id) ?? 0) + 1);
  const matched = new Set<number>();
  for (let i = up.length - 1; i >= 0; i--) {
    const count = available.get(up[i].tid) ?? 0;
    if (!count) continue;
    available.set(up[i].tid, count - 1);
    matched.add(i);
  }
  return matched;
}

function filterUpcoming(up: SongQueueEntry[], upcoming: string[], now: number): SongQueueEntry[] {
  const matched = matchUpcoming(up, upcoming);
  const lastMatch = Math.max(-1, ...matched);
  const cutoff = now - 15_000;
  return up.filter((entry, index) => {
    if (matched.has(index)) return true;
    if (index < lastMatch) return false;
    if (upcoming.length >= 20) return true;
    return entry.at <= 0 || entry.at >= cutoff;
  });
}
