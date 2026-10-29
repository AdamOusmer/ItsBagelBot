// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MAX_QUEUE_ENTRIES = 100;

export type QueueEntry = { login: string; joinedAt: number };

export type QueueView = { open: boolean; total: number; entries: QueueEntry[]; degraded: boolean };

export type QueueSnapshot = QueueView & { at: number };

export const degradedQueue = (): QueueView => ({ open: false, total: 0, entries: [], degraded: true });

export const snapshotOf = (view: QueueView, at: number): QueueSnapshot => ({ ...view, at });

function validEntry(raw: unknown): raw is QueueEntry {
  const entry = raw as Partial<QueueEntry> | null;
  return typeof entry?.login === 'string' && entry.login !== '' && Number.isFinite(entry.joinedAt);
}

export function parseSnapshot(raw: unknown): QueueSnapshot | null {
  const snap = raw as Partial<QueueSnapshot> | null;
  if (!snap || typeof snap.open !== 'boolean' || typeof snap.degraded !== 'boolean') return null;
  if (!Number.isFinite(snap.total) || !Number.isFinite(snap.at)) return null;
  if (!Array.isArray(snap.entries) || !snap.entries.every(validEntry)) return null;
  return { open: snap.open, degraded: snap.degraded, total: snap.total as number, at: snap.at as number, entries: snap.entries };
}

export function waitedSeconds(entry: QueueEntry, nowMs: number): number {
  return Math.max(0, Math.floor((nowMs - entry.joinedAt) / 1000));
}
