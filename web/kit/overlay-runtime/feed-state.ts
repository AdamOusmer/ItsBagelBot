// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { track } from './data-widgets';
import type { AlertControl, FeedEvent, FeedSnapshot, FeedStats, OverlayEvent, Subathon } from './feed';

export class FeedState {
  snapshot: FeedSnapshot = { stats: {}, subathon: {}, control: { paused: false, muted: false, skip: 0 } };
  #seq = 0;

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
    this.snapshot = { ...this.snapshot, [field]: track(this.snapshot[field] ?? [], event, field === 'chat' ? 100 : 200) };
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
