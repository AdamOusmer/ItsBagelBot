// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { formatCounterValue, parseCounterValue } from '@bagel/kit/validation';
import type { PublicStats } from './server/public-stats';
import type { PublicBoards } from './server/public-boards';

function record(raw: unknown): Record<string, unknown> | null {
  return raw !== null && typeof raw === 'object' && !Array.isArray(raw)
    ? raw as Record<string, unknown>
    : null;
}

function rate(raw: unknown): raw is number | null {
  return raw === null || (typeof raw === 'number' && Number.isFinite(raw) && raw >= 0);
}

function validRates(stats: Record<string, unknown>): boolean {
  return ['msg_rate', 'event_rate', 'msg_rate_now', 'event_rate_now']
    .every((key) => rate(stats[key]));
}

export function validStats(raw: unknown): PublicStats | null {
  const stats = record(raw);
  if (!stats) return null;
  if (typeof stats.degraded !== 'boolean') return null;
  if (!validRates(stats)) return null;
  const messages = parseCounterValue(stats.messages_total);
  const events = parseCounterValue(stats.events_total);
  if (messages === null || events === null) return null;
  return { ...stats, messages_total: messages, events_total: events } as unknown as PublicStats;
}

function channelRow(raw: unknown): PublicBoards['channels'][number] | null {
  const row = record(raw);
  if (!row || typeof row.id !== 'string' || typeof row.name !== 'string') return null;
  const messages = parseCounterValue(row.messages);
  const events = parseCounterValue(row.events);
  if (messages === null || events === null) return null;
  return { id: row.id, name: row.name, messages, events };
}

function channelRows(raw: unknown): PublicBoards['channels'] | null {
  if (!Array.isArray(raw)) return null;
  const channels: PublicBoards['channels'] = [];
  for (const rawRow of raw) {
    const row = channelRow(rawRow);
    if (!row) return null;
    channels.push(row);
  }
  return channels;
}

function feedEntry(raw: unknown): PublicBoards['feed']['entries'][number] | null {
  const row = record(raw);
  if (!row || typeof row.id !== 'string' || typeof row.name !== 'string') return null;
  const count = parseCounterValue(row.count);
  return count === null ? null : { id: row.id, name: row.name, count };
}

function feedRows(raw: unknown): PublicBoards['feed']['entries'] | null {
  if (!Array.isArray(raw)) return null;
  const entries: PublicBoards['feed']['entries'] = [];
  for (const rawRow of raw) {
    const row = feedEntry(rawRow);
    if (!row) return null;
    entries.push(row);
  }
  return entries;
}

function feedBoard(raw: unknown): PublicBoards['feed'] | null {
  const feed = record(raw);
  if (!feed) return null;
  const total = parseCounterValue(feed.total);
  const ranked = parseCounterValue(feed.ranked);
  const entries = feedRows(feed.entries);
  if (total === null || ranked === null || entries === null) return null;
  return { total, ranked, entries };
}

export function validBoards(raw: unknown): PublicBoards | null {
  const boards = record(raw);
  if (!boards || typeof boards.degraded !== 'boolean') return null;
  const channels = channelRows(boards.channels);
  const feed = feedBoard(boards.feed);
  if (!channels || !feed) return null;
  return { channels, feed, degraded: boards.degraded };
}

export function exactDisplay(raw: number): number {
  return Number.isFinite(raw) ? Math.max(0, Math.min(raw, Number.MAX_SAFE_INTEGER)) : 0;
}

export function formatStatTotal(raw: string, animated: number, locale?: string): string {
  return BigInt(raw) > BigInt(Number.MAX_SAFE_INTEGER)
    ? formatCounterValue(raw, locale)
    : new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(Math.round(animated));
}
