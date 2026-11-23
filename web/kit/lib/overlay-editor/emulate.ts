// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AlertKind, FeedEvent, Identity, OverlayEvent } from '../../overlay-runtime/feed';

type Sample = Omit<OverlayEvent, 'id' | 'kind' | 'at' | 'user'>;

const VIEWER: Identity = { id: '1001', login: 'bagelfan', name: 'BagelFan' };
const GIFTEE: Identity = { id: '1002', login: 'toasty', name: 'Toasty' };

const samples: Record<AlertKind, Sample> = {
  follow: {},
  sub: { tier: '1000', message: { text: 'Happy to be here' } },
  resub: { tier: '1000', months: 12, streak: 6, message: { text: 'One year already' } },
  gift_sub: { tier: '1000', amount: 1, recipient: GIFTEE },
  community_gift: { tier: '1000', amount: 5 },
  cheer: { amount: 500, message: { text: 'Cheer500 keep going' } },
  raid: { amount: 42 },
  redemption: { reward: { id: 'sample', title: 'Hydrate' } }
};

export function sampleEvent(kind: AlertKind, seq: number, now: number = Date.now()): FeedEvent {
  return { ...samples[kind], id: `test-${seq}`, kind, at: new Date(now).toISOString(), user: VIEWER, seq, test: true };
}

export class EmulationBus {
  #listeners = new Set<(event: FeedEvent) => void>();
  #seq = 0;

  subscribe(listener: (event: FeedEvent) => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  emit(kind: AlertKind): FeedEvent {
    const event = sampleEvent(kind, ++this.#seq);
    this.#listeners.forEach((listener) => listener(event));
    return event;
  }
}
