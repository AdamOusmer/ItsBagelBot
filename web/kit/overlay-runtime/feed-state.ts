// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { track } from './data-widgets';
import type { AlertControl, FeedEvent, FeedSnapshot, FeedStats, OverlayEvent, Subathon } from './feed';

export const MIN_EVENTS = 200;
export const MIN_CHAT = 100;

export class FeedState {
  limits = { events: MIN_EVENTS, chat: MIN_CHAT };
  snapshot: FeedSnapshot = { stats: {}, subathon: {}, control: { paused: false, muted: false, skip: 0 } };
  #seq = 0;

  load(snapshot: FeedSnapshot): void {
    const number = (items?: FeedEvent[] | null) => items?.map((item) => ({ ...item, seq: ++this.#seq }));
    this.snapshot = { ...snapshot, chat: number(snapshot.chat), events: number(snapshot.events) };
  }

  ingest(event: OverlayEvent): FeedEvent {
    const fed = { ...event, seq: ++this.#seq };
    this.event(fed);
    return fed;
  }

  event(event: FeedEvent): void {
    if (event.test || event.replay) return;
    if (event.kind === 'user_cleared') {
      this.snapshot = { ...this.snapshot, chat: (this.snapshot.chat ?? []).filter((item) => item.user?.id !== event.user?.id) };
      return;
    }
    const field = event.kind === 'chat' ? 'chat' : 'events';
    this.snapshot = { ...this.snapshot, [field]: track(this.snapshot[field] ?? [], event, this.limits[field]) };
  }

  stats(value: FeedStats): void {
    this.snapshot = { ...this.snapshot, stats: value };
  }

  subathon(value: Subathon): void {
    this.snapshot = { ...this.snapshot, subathon: value };
  }

  control(value: AlertControl): void {
    this.snapshot = { ...this.snapshot, control: value };
  }
}
