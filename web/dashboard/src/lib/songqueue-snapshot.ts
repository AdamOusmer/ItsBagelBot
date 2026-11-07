// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MAX_SONG_ROWS = 10;

export type SongRow = { title: string; artists: string; requester: string; joinedAt: number };

export type SongQueuePublicView = { open: boolean; current: SongRow | null; up: SongRow[]; total: number; degraded: boolean };

export type SongQueueSnapshot = SongQueuePublicView & { at: number };

export const degradedSongQueue = (): SongQueuePublicView => ({ open: false, current: null, up: [], total: 0, degraded: true });

export const songSnapshotOf = (view: SongQueuePublicView, at: number): SongQueueSnapshot => ({ ...view, at });

function validRow(raw: unknown): raw is SongRow {
  const row = raw as Partial<SongRow> | null;
  return typeof row?.title === 'string' && typeof row.artists === 'string' && typeof row.requester === 'string' && Number.isFinite(row.joinedAt);
}

const SNAPSHOT_CHECKS: readonly ((snap: Partial<SongQueueSnapshot>) => boolean)[] = [
  (s) => typeof s.open === 'boolean',
  (s) => typeof s.degraded === 'boolean',
  (s) => Number.isFinite(s.total),
  (s) => Number.isFinite(s.at),
  (s) => s.current === null || validRow(s.current),
  (s) => Array.isArray(s.up) && s.up.every(validRow)
];

function validSnapshot(raw: unknown): raw is SongQueueSnapshot {
  const snap = (raw ?? {}) as Partial<SongQueueSnapshot>;
  return SNAPSHOT_CHECKS.every((check) => check(snap));
}

export function parseSongSnapshot(raw: unknown): SongQueueSnapshot | null {
  if (!validSnapshot(raw)) return null;
  const { open, current, up, total, degraded, at } = raw;
  return { open, current, up, total, degraded, at };
}
