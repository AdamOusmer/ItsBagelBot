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

const SNAPSHOT_CHECKS: readonly ((snap: Partial<QueueSnapshot>) => boolean)[] = [
  (s) => typeof s.open === 'boolean',
  (s) => typeof s.degraded === 'boolean',
  (s) => Number.isFinite(s.total),
  (s) => Number.isFinite(s.at),
  (s) => Array.isArray(s.entries) && s.entries.every(validEntry)
];

function validSnapshot(raw: unknown): raw is QueueSnapshot {
  const snap = (raw ?? {}) as Partial<QueueSnapshot>;
  return SNAPSHOT_CHECKS.every((check) => check(snap));
}

export function parseSnapshot(raw: unknown): QueueSnapshot | null {
  if (!validSnapshot(raw)) return null;
  const { open, degraded, total, at, entries } = raw;
  return { open, degraded, total, at, entries };
}

export function waitedSeconds(entry: QueueEntry, nowMs: number): number {
  return Math.max(0, Math.floor((nowMs - entry.joinedAt) / 1000));
}
