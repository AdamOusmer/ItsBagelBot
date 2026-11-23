// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayWidget, WidgetKind } from '../overlay-document';

export type Settings = Record<string, unknown>;
export type Canvas = { width: number; height: number };

export const EDITABLE_KINDS = [
  'alertbox', 'eventlist', 'goal', 'label', 'chatbox', 'emotewall', 'timer', 'credits', 'text', 'image', 'video', 'slideshow'
] as const satisfies readonly WidgetKind[];
export type EditableKind = (typeof EDITABLE_KINDS)[number];

export const ALERT_KINDS = ['follow', 'sub', 'resub', 'gift_sub', 'community_gift', 'cheer', 'raid', 'redemption'] as const;
export type AlertKind = (typeof ALERT_KINDS)[number];

const TIMER_LEAD_MS = 5 * 60_000;
const ALERT_TITLE_SIZE = 52;

export const alertTitles: Record<AlertKind, string> = {
  follow: '{name} followed',
  sub: '{name} subscribed',
  resub: '{name} subscribed for {months} months',
  gift_sub: '{name} gifted a sub to {recipient}',
  community_gift: '{name} gifted {count} subs',
  cheer: '{name} cheered {amount} bits',
  raid: '{name} raided with {amount} viewers',
  redemption: '{name} redeemed {reward}'
};

export const fontStyle = {
  family: 'System sans', size: 36, weight: 700, color: '#ffffff',
  stroke: 0, strokeColor: '#000000', shadow: 4, shadowColor: '#000000'
};

function byAlert(make: (kind: AlertKind) => unknown): Settings {
  return Object.fromEntries(ALERT_KINDS.map((kind) => [kind, make(kind)]));
}

function alertConfig(kind: AlertKind): Settings {
  return {
    enabled: true, title: alertTitles[kind], media: '', sound: '', showMessage: true, volume: 0.8,
    duration: 5, textDelay: 0, layout: 'above', enter: 'pop', exit: 'fade', enterDuration: 0.35,
    exitDuration: 0.35, style: { ...fontStyle, size: ALERT_TITLE_SIZE }, minAmount: 0, priority: 0,
    variations: kind === 'community_gift'
      ? [{ condition: 'amount_exact', value: '1', overrides: { title: '{name} gifted a sub', style: {} } }]
      : []
  };
}

export const defaultSettings: Record<EditableKind, Settings> = {
  alertbox: { events: byAlert(alertConfig), queueDelay: 0.5, interruptThreshold: 0, thirdPartyEmotes: true },
  eventlist: {
    count: 5, direction: 'newest_first', fade: 0, style: fontStyle,
    rules: byAlert((kind) => ({ enabled: true, minAmount: 0, template: alertTitles[kind] }))
  },
  goal: {
    metric: 'session_follows', start: 0, target: 25, manual: 0, title: 'Follower goal', display: 'horizontal',
    fill: '#8bc583', track: '#222b29', style: fontStyle, weights: { '1000': 1, '2000': 2, '3000': 6, prime: 1 }
  },
  label: { source: 'latest_follow', template: 'Latest follower: {name}', empty: 'Waiting for a follower', marquee: true, style: fontStyle },
  chatbox: {
    count: 8, fade: 30, direction: 'newest_last', hideCommands: true, hideUsers: [], badges: true, nameColor: '#a5df9a',
    nativeColors: true, emotes: true, thirdPartyEmotes: true, theme: 'clean', style: { ...fontStyle, size: 30, weight: 500 }
  },
  emotewall: { motion: 'fly', max: 24, size: 72, lifetime: 4, combo: 5, thirdPartyEmotes: true, style: fontStyle },
  timer: { mode: 'countdown', at: '', format: 'hh:mm:ss', endText: 'Starting now', style: { ...fontStyle, size: 96 } },
  credits: {
    headers: { follow: 'Followers', sub: 'Subscribers', gift_sub: 'Gifters', cheer: 'Cheerers', raid: 'Raiders', redemption: 'Redeemers', mod: 'Moderators', vip: 'VIPs', chatter: 'Chatters' },
    showMods: false, showVips: false, showChatters: false, speed: 45, loop: true, style: fontStyle
  },
  text: { text: 'Your text', align: 'center', style: fontStyle },
  image: { media: '', fit: 'contain' },
  video: { media: '', fit: 'contain', loop: true, muted: true },
  slideshow: { images: [], interval: 6, fade: 0.5, fit: 'contain' }
};

export function isEditable(kind: string): kind is EditableKind {
  return (EDITABLE_KINDS as readonly string[]).includes(kind);
}

const isPlain = (value: unknown): value is Settings => typeof value === 'object' && value !== null && !Array.isArray(value);

function merge(base: unknown, over: unknown): unknown {
  if (!isPlain(base) || !isPlain(over)) return over === undefined ? base : over;
  const keys = new Set([...Object.keys(base), ...Object.keys(over)]);
  return Object.fromEntries([...keys].map((key) => [key, merge(base[key], over[key])]));
}

const TWITCH_USER_ID = /^\d{1,20}$/;

function migrated(kind: string, settings: Settings): Settings {
  if (kind !== 'chatbox' || !Array.isArray(settings.hideUsers)) return settings;
  return { ...settings, hideUsers: settings.hideUsers.filter((entry) => typeof entry === 'string' && TWITCH_USER_ID.test(entry.trim())) };
}

export function completeSettings(kind: string, settings: Settings | null): Settings {
  const base = isEditable(kind) ? defaultSettings[kind] : {};
  return migrated(kind, merge(base, settings ?? {}) as Settings);
}

export function newWidget(kind: EditableKind, canvas: Canvas, name: string): OverlayWidget {
  const w = Math.round(canvas.width * 0.45);
  const h = Math.round(canvas.height * 0.28);
  const settings = structuredClone(defaultSettings[kind]);
  if (kind === 'timer') settings.at = new Date(Date.now() + TIMER_LEAD_MS).toISOString();
  return {
    id: crypto.randomUUID(), kind, name,
    x: Math.round((canvas.width - w) / 2), y: Math.round((canvas.height - h) / 2), w, h,
    rotation: 0, opacity: 1, hidden: false, locked: false, settings
  };
}
