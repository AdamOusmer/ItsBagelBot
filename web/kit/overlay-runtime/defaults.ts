// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { alertKinds, type AlertKind } from './feed';
import type { AlertConfig, FontStyle, WidgetSettings } from './widget-types';

export const titles: Record<AlertKind, string> = {
  follow: '{name} followed',
  sub: '{name} subscribed',
  resub: '{name} subscribed for {months} months',
  gift_sub: '{name} gifted a sub to {recipient}',
  community_gift: '{name} gifted {count} subs',
  cheer: '{name} cheered {amount} bits',
  raid: '{name} raided with {amount} viewers',
  redemption: '{name} redeemed {reward}'
};

export const font: FontStyle = {
  family: 'System sans',
  size: 36,
  weight: 700,
  color: '#ffffff',
  stroke: 0,
  strokeColor: '#000000',
  shadow: 4,
  shadowColor: '#000000'
};

function alertConfig(kind: AlertKind): AlertConfig {
  return {
    enabled: true,
    title: titles[kind],
    media: '',
    sound: '',
    showMessage: true,
    volume: 0.8,
    duration: 5,
    textDelay: 0,
    layout: 'above',
    enter: 'pop',
    exit: 'fade',
    enterDuration: 0.35,
    exitDuration: 0.35,
    style: { ...font, size: 52 },
    minAmount: 0,
    priority: 0,
    variations:
      kind === 'community_gift'
        ? [{ condition: 'amount_exact', value: '1', overrides: { title: '{name} gifted a sub', style: {} } }]
        : []
  };
}

function byAlert<T>(make: (kind: AlertKind) => T): Record<AlertKind, T> {
  return Object.fromEntries(alertKinds.map((kind) => [kind, make(kind)])) as Record<AlertKind, T>;
}

export const settings: WidgetSettings = {
  alertbox: { events: byAlert(alertConfig), queueDelay: 0.5, interruptThreshold: 0 },
  eventlist: {
    count: 5,
    direction: 'newest_first',
    rules: byAlert((kind) => ({ enabled: true, minAmount: 0, template: titles[kind] })),
    fade: 0,
    style: font
  },
  goal: {
    metric: 'session_follows',
    start: 0,
    target: 25,
    manual: 0,
    title: 'Follower goal',
    display: 'horizontal',
    fill: '#8bc583',
    track: '#222b29',
    style: font,
    weights: { '1000': 1, '2000': 2, '3000': 6, prime: 1 }
  },
  label: {
    source: 'latest_follow',
    template: 'Latest follower: {name}',
    empty: 'Waiting for a follower',
    marquee: true,
    style: font
  },
  chatbox: {
    count: 8,
    fade: 30,
    direction: 'newest_last',
    hideCommands: true,
    hideUsers: [],
    badges: true,
    nameColor: '#a5df9a',
    nativeColors: true,
    emotes: true,
    theme: 'clean',
    style: { ...font, size: 30, weight: 500 }
  },
  emotewall: { motion: 'fly', max: 24, size: 72, lifetime: 4, combo: 5, style: font },
  timer: { mode: 'countdown', at: '', format: 'hh:mm:ss', endText: 'Starting now', style: { ...font, size: 96 } },
  credits: {
    headers: {
      follow: 'Followers',
      sub: 'Subscribers',
      gift_sub: 'Gifters',
      cheer: 'Cheerers',
      raid: 'Raiders',
      redemption: 'Redeemers'
    },
    speed: 45,
    loop: true,
    style: font
  },
  text: { text: 'Your text', align: 'center', style: font },
  image: { media: '', fit: 'contain' },
  video: { media: '', fit: 'contain', loop: true, muted: true },
  slideshow: { images: [], interval: 6, fade: 0.5, fit: 'contain' },
  custom: { bundle: '', values: {} }
};
