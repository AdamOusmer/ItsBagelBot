// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AuditEntry } from '#lib/server/services';
import type { GiveawayWinnerWire } from '#lib/server/giveaways';

type UserHistory<T> = { entries: T[] | null; error: string };
type AuditHistory = { entries?: AuditEntry[] };
type PrizeHistory = { awards?: GiveawayWinnerWire[] };

export const EMPTY_HISTORY = { entries: null, error: '' };

async function readHistory<T>(url: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(url, { signal });
  const body = (await response.json()) as T & { error?: string };
  if (body.error) throw new Error(body.error);
  if (!response.ok) throw new Error(`history fetch failed (${response.status})`);
  return body;
}

async function historyResult<T>(read: Promise<T[]>): Promise<UserHistory<T>> {
  try {
    return { entries: await read, error: '' };
  } catch (error) {
    return { entries: [], error: error instanceof Error ? error.message : String(error) };
  }
}

export function inspectAudit(id: string, signal: AbortSignal) {
  const query = new URLSearchParams({ q: id });
  return historyResult(
    readHistory<AuditHistory>(`/audit/data?${query}`, signal).then(
      ({ entries }) => (entries ?? []).filter((entry) => entry.target === id).slice(0, 5)
    )
  );
}

export function inspectPrizes(id: string, signal: AbortSignal) {
  const query = new URLSearchParams({ q: id });
  return historyResult(
    readHistory<PrizeHistory>(`/users/prizes?${query}`, signal).then(({ awards }) => awards ?? [])
  );
}
