// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FeedEvent } from './feed';

const windowMs = 2000;
type Entry = { event: FeedEvent; seen: number };
type Budget = Entry & { remaining: number };

function eligible(event: FeedEvent, kind: FeedEvent['kind']): boolean {
  return event.kind === kind && !event.replay;
}

function gifter(event: FeedEvent): string {
  return event.anonymous ? 'anonymous' : (event.user?.id || event.user?.login || '');
}

function withinWindow(budget: Entry, gift: Entry): boolean {
  const communityAt = Date.parse(budget.event.at) || budget.seen;
  const giftAt = Date.parse(gift.event.at) || gift.seen;
  return Math.abs(communityAt - giftAt) <= windowMs && Math.abs(budget.seen - gift.seen) <= windowMs;
}

function sameTestness(a: FeedEvent, b: FeedEvent): boolean {
  return Boolean(a.test) === Boolean(b.test);
}

function sameGifter(budget: Budget, gift: Entry): boolean {
  return !gift.event.user || gifter(budget.event) === gifter(gift.event);
}

function matches(budget: Budget, gift: Entry): boolean {
  if (budget.remaining <= 0 || !withinWindow(budget, gift)) return false;
  return sameTestness(budget.event, gift.event) && sameGifter(budget, gift);
}

export class GiftBurst {
  #gifts: Entry[] = [];
  #budgets: Budget[] = [];

  push(event: FeedEvent, now: number): FeedEvent[] {
    const ready = this.flush(now);
    const entry = { event, seen: now };
    if (eligible(event, 'gift_sub')) this.#single(entry);
    else ready.push(event);
    if (eligible(event, 'community_gift')) this.#community(entry);
    return ready;
  }

  flush(now: number): FeedEvent[] {
    const ready = this.#gifts.filter(item => now - item.seen > windowMs);
    this.#gifts = this.#gifts.slice(ready.length);
    this.#budgets = this.#budgets.filter(item => now - item.seen <= windowMs && item.remaining > 0);
    return ready.map(item => item.event);
  }

  #single(entry: Entry): void {
    const budget = this.#budgets.find(item => matches(item, entry));
    if (budget) budget.remaining--;
    else this.#gifts.push(entry);
  }

  #community(entry: Entry): void {
    const budget = { ...entry, remaining: entry.event.amount ?? 0 };
    this.#gifts = this.#gifts.filter(item => {
      if (!matches(budget, item)) return true;
      budget.remaining--;
      return false;
    });
    if (budget.remaining > 0) this.#budgets.push(budget);
  }
}

export function mergeGiftBursts(events: readonly FeedEvent[]): FeedEvent[] {
  const burst = new GiftBurst();
  let seen = 0;
  const merged = events.toSorted((a, b) => a.seq - b.seq).flatMap(event => {
    seen = Math.max(seen, Date.parse(event.at) || seen);
    return burst.push(event, seen);
  });
  return [...merged, ...burst.flush(Infinity)].sort((a, b) => a.seq - b.seq);
}