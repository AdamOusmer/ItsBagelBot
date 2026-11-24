// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { displayName } from './events';
import type { EventKind, FeedStats, OverlayEvent } from './feed';

type Detail = Record<string, unknown>;
export type CompatEvent = { name: string; detail: Detail };

const who = (event?: OverlayEvent): string => (event?.anonymous ? 'Anonymous' : displayName(event?.user));
const note = (event: OverlayEvent): string => event.message?.text ?? '';
const subscription = (event: OverlayEvent): [string, Detail] => [
  'subscriber-latest',
  { name: who(event), amount: event.months || 1, tier: event.tier ?? '', message: note(event) }
];

type Listener = (event: OverlayEvent) => [string, Detail];

const listeners: Partial<Record<EventKind, Listener>> = {
  follow: (event) => ['follower-latest', { name: who(event) }],
  sub: subscription,
  resub: subscription,
  gift_sub: (event) => [
    'subscriber-latest',
    { name: displayName(event.recipient), sender: who(event), gifted: true, amount: 1, tier: event.tier ?? '' }
  ],
  community_gift: (event) => [
    'subscriber-latest',
    { name: who(event), amount: event.amount ?? 0, bulkGifted: true, tier: event.tier ?? '' }
  ],
  cheer: (event) => ['cheer-latest', { name: who(event), amount: event.amount ?? 0, message: note(event) }],
  raid: (event) => ['raid-latest', { name: who(event), amount: event.amount ?? 0 }],
  redemption: (event) => [
    'redemption-latest',
    { name: who(event), amount: 1, item: event.reward?.title ?? '', message: note(event) }
  ],
  chat: (event) => [
    'message',
    {
      data: {
        text: note(event),
        displayName: displayName(event.user),
        nick: event.user?.login ?? '',
        userId: event.user?.id ?? '',
        msgId: event.id,
        badges: (event.badges ?? []).map((type) => ({ type }))
      }
    }
  ],
  user_cleared: (event) => ['delete-messages', { userId: event.user?.id ?? '' }]
};

function latest(stats: FeedStats, kind: EventKind): Detail {
  const event = stats.latest?.[kind];
  return { name: event ? who(event) : '', amount: event?.amount ?? 0 };
}

function sessionData(stats: FeedStats): Detail {
  const counts = stats.session ?? {};
  return {
    'follower-session': { count: counts.follows ?? 0 },
    'follower-latest': latest(stats, 'follow'),
    'subscriber-session': { count: counts.subs ?? 0 },
    'subscriber-latest': latest(stats, 'sub'),
    'cheer-session': { amount: counts.bits ?? 0 },
    'cheer-latest': latest(stats, 'cheer'),
    'raid-latest': latest(stats, 'raid')
  };
}

const knownListeners = Object.entries(listeners).filter((entry): entry is [string, Listener] => entry[1] !== undefined);

export function compatEvent(event: OverlayEvent): CompatEvent | undefined {
  const known = knownListeners.find(([kind]) => kind === event.kind);
  if (!known) return;
  const [listener, data] = known[1](event);
  return { name: 'onEventReceived', detail: { listener, event: data } };
}

export function compatLoad(fields: Detail, stats: FeedStats): CompatEvent {
  return { name: 'onWidgetLoad', detail: { fieldData: fields, session: { data: sessionData(stats) } } };
}

export function compatSession(stats: FeedStats): CompatEvent {
  return { name: 'onSessionUpdate', detail: { session: { data: sessionData(stats) } } };
}
