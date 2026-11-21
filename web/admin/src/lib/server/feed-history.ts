// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
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

function unrecordedLive(replay: FeedRecord[], live: FeedRecord[]): FeedRecord[] {
  const overlap = overlapCounts(replay);
  return live.filter((event) => {
    const key = recordKey(event);
    const copies = overlap.get(key) ?? 0;
    if (!copies) return true;
    overlap.set(key, copies - 1);
    return false;
  });
}

function preview(payload: string): string {
  const text = payload.trim();
  return text.length > PAYLOAD_LIMIT ? text.slice(0, PAYLOAD_LIMIT) + '…' : text;
}

export class FeedHistory {
  readonly generation = randomUUID();
  private readonly events = new FeedRing<FeedEvent>();
  private readonly listeners = new Set<FeedListener>();
  private pending?: FeedRing<FeedRecord>;
  private seeding?: Promise<void>;
  private sequence = 0;

  seed(load: () => Promise<FeedRecord[]>): Promise<void> {
    this.seeding ??= this.loadSeed(load);
    return this.seeding;
  }

  private async loadSeed(load: () => Promise<FeedRecord[]>): Promise<void> {
    const pending = new FeedRing<FeedRecord>();
    this.pending = pending;
    let replay: FeedRecord[] = [];
    try {
      replay = await load();
    } catch (err) {
      logger.warn({ err }, 'admin event history unavailable');
    }
    this.pending = undefined;
    for (const event of [...replay, ...unrecordedLive(replay, pending.values())]) this.add(event);
  }

  add(record: FeedRecord): void {
    if (this.pending) {
      this.pending.add(record);
      return;
    }
    const event = { ...record, id: ++this.sequence, payload: preview(record.payload) };
    this.events.add(event);
    for (const listener of this.listeners) this.notify(listener, () => listener.feed(event));
  }

  listen(listener: FeedListener): () => void {
    this.listeners.add(listener);
    this.notify(listener, () => listener.history(this.events.values().reverse()));
    return () => this.listeners.delete(listener);
  }

  disconnect(): void {
    for (const listener of this.listeners) this.notify(listener, () => listener.close());
    this.listeners.clear();
  }

  private notify(listener: FeedListener, deliver: () => void): void {
    try {
      deliver();
    } catch (err) {
      this.listeners.delete(listener);
      logger.warn({ err }, 'admin event listener failed');
    }
  }
}
