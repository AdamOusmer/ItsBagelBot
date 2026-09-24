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

export function validStats(raw: unknown): PublicStats | null {
  const stats = record(raw);
  const messages = parseCounterValue(stats?.messages_total);
  const events = parseCounterValue(stats?.events_total);
  if (!stats || typeof stats.degraded !== 'boolean' ||
    messages === null || events === null ||
    !rate(stats.msg_rate) || !rate(stats.event_rate) ||
    !rate(stats.msg_rate_now) || !rate(stats.event_rate_now)) return null;
  return { ...stats, messages_total: messages, events_total: events } as unknown as PublicStats;
}

export function validBoards(raw: unknown): PublicBoards | null {
  const boards = record(raw);
  const feed = record(boards?.feed);
  const total = parseCounterValue(feed?.total);
  const ranked = parseCounterValue(feed?.ranked);
  if (!boards || typeof boards.degraded !== 'boolean' || !Array.isArray(boards.channels) ||
    !feed || total === null || ranked === null ||
    !Array.isArray(feed.entries)) return null;
  const channels: PublicBoards['channels'] = [];
  for (const rawRow of boards.channels) {
    const row = record(rawRow);
    const messages = parseCounterValue(row?.messages);
    const events = parseCounterValue(row?.events);
    if (!row || typeof row.id !== 'string' || typeof row.name !== 'string' ||
      messages === null || events === null) return null;
    channels.push({ id: row.id, name: row.name, messages, events });
  }
  const entries: PublicBoards['feed']['entries'] = [];
  for (const rawRow of feed.entries) {
    const row = record(rawRow);
    const count = parseCounterValue(row?.count);
    if (!row || typeof row.id !== 'string' || typeof row.name !== 'string' ||
      count === null) return null;
    entries.push({ id: row.id, name: row.name, count });
  }
  return { channels, feed: { total, ranked, entries }, degraded: boards.degraded };
}

export function exactDisplay(raw: number): number {
  return Number.isFinite(raw) ? Math.max(0, Math.min(raw, Number.MAX_SAFE_INTEGER)) : 0;
}

export function formatStatTotal(raw: string, animated: number, locale?: string): string {
  return BigInt(raw) > BigInt(Number.MAX_SAFE_INTEGER)
    ? formatCounterValue(raw, locale)
    : new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(Math.round(animated));
}
