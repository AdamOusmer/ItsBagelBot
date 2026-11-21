// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { logger } from '@bagel/kit/server/logger';

export interface FeedRecord {
  at: string;
  subject: string;
  label: string;
  tone: 'up' | 'down' | 'neutral';
  payload: string;
}

export type FeedEvent = FeedRecord & { id: number };

export interface FeedListener {
  history: (events: FeedEvent[]) => void;
  feed: (event: FeedEvent) => void;
  close: () => void;
}

const CAPACITY = 500;
const PAYLOAD_LIMIT = 240;

export class FeedRing<T> {
  private readonly entries: T[] = [];
  private next = 0;

  add(value: T): void {
    this.entries[this.next] = value;
    this.next = (this.next + 1) % CAPACITY;
  }

  values(): T[] {
    return [...this.entries.slice(this.next), ...this.entries.slice(0, this.next)];
  }
}

function recordKey(event: FeedRecord): string {
  return JSON.stringify([event.subject, event.payload]);
}

function overlapCounts(events: FeedRecord[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const event of events) {
    const key = recordKey(event);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function unobservedReplay(replay: FeedRecord[], live: FeedRecord[], since: string): FeedRecord[] {
  const overlap = overlapCounts(live);
  const unobserved: FeedRecord[] = [];
  for (const event of [...replay].reverse()) {
    const key = recordKey(event);
    const copies = event.at >= since ? overlap.get(key) : undefined;
    if (copies) overlap.set(key, copies - 1);
    else unobserved.push(event);
  }
  return unobserved.reverse();
}

function preview(payload: string): string {
  const text = payload.trim();
  return text.length > PAYLOAD_LIMIT ? text.slice(0, PAYLOAD_LIMIT) + '…' : text;
}

export class FeedHistory {
  private readonly events = new FeedRing<FeedEvent>();
  private readonly listeners = new Set<FeedListener>();
  private pending?: FeedRing<FeedRecord>;
  private seeding?: Promise<void>;
  private sequence = 0;

  seed(since: string, load: () => Promise<FeedRecord[]>): Promise<void> {
    this.seeding ??= this.loadSeed(since, load);
    return this.seeding;
  }

  private async loadSeed(since: string, load: () => Promise<FeedRecord[]>): Promise<void> {
    const pending = new FeedRing<FeedRecord>();
    this.pending = pending;
    let replay: FeedRecord[] = [];
    try {
      replay = await load();
    } catch (err) {
      logger.warn({ err }, 'admin event history unavailable');
    }
    this.pending = undefined;
    const live = pending.values();
    const merged = [...unobservedReplay(replay, live, since), ...live];
    for (const event of merged.sort((a, b) => a.at.localeCompare(b.at))) this.add(event);
  }

  add(record: FeedRecord): void {
    if (this.pending) {
      this.pending.add(record);
      return;
    }
    const event = { ...record, id: ++this.sequence, payload: preview(record.payload) };
    this.events.add(event);
    for (const listener of this.listeners) listener.feed(event);
  }

  listen(listener: FeedListener): () => void {
    listener.history(this.events.values().reverse());
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  disconnect(): void {
    for (const listener of this.listeners) listener.close();
    this.listeners.clear();
  }
}
