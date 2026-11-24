// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { WidgetKind } from '../overlay-document';
import { fontFamilies } from './fonts';

export const animations = ['none', 'fade', 'slide-up', 'slide-down', 'slide-left', 'slide-right', 'zoom', 'pop', 'bounce', 'flip'] as const;
export const layouts = ['above', 'below', 'left', 'right', 'overlay'] as const;
export const conditions = [
  'amount_min', 'amount_exact', 'tier', 'months_min', 'months_exact', 'streak_min', 'count_min', 'message_contains',
  'anonymous', 'is_gift', 'prime', 'upgrade', 'random'
] as const;
const periods = ['session', 'week', 'month', 'all_time'] as const;
const counters = ['follows', 'subs', 'gifts', 'bits'] as const;
const counted = periods.flatMap((period) => counters.map((counter) => `${period}_${counter}` as const));
export const metrics = [...counted, 'sub_points', 'manual'] as const;
export const sources = [
  'latest_follow', 'latest_sub', 'latest_resub', 'latest_cheer', 'latest_raid', 'latest_gift', 'latest_redemption',
  ...counted, 'session_resubs', 'session_raids', 'session_redemptions',
  'top_cheerer', 'top_gifter', 'all_time_top_cheerer', 'all_time_top_gifter'
] as const;
export const tiers = ['1000', '2000', '3000', 'prime'] as const;
export const directions = ['newest_first', 'newest_last'] as const;
const fit = ['contain', 'cover', 'fill'] as const;
const clockFormats = ['hh:mm:ss', 'mm:ss', 'seconds'] as const;

export type MediaSlot = 'image' | 'visual' | 'video' | 'sound';
export type FieldFormat = 'text' | 'area' | 'color' | 'media' | 'media-list' | 'list' | 'datetime' | 'boolean' | 'number' | 'select';

export type SettingField = {
  key: string;
  type: FieldFormat;
  label?: string;
  options?: readonly string[];
  raw?: boolean;
  slot?: MediaSlot;
  min?: number;
  max?: number;
  step?: number;
};

const UNBOUNDED = Number.MAX_SAFE_INTEGER;

const field = (key: string, type: FieldFormat = 'text', label?: string): SettingField => ({ key, type, label });
const num = (key: string, range: [number, number], step = 1, label?: string): SettingField => ({
  key, label, type: 'number', min: range[0], max: range[1], step
});
const choice = (key: string, options: readonly string[], raw = false): SettingField => ({ key, type: 'select', options, raw });
const seconds = (key: string, min = 0, max = 300, label?: string): SettingField => num(key, [min, max], 0.1, label);
const media = (key: string, slot: MediaSlot, type: FieldFormat = 'media'): SettingField => ({ key, type, slot });
const thirdPartyEmotes = field('thirdPartyEmotes', 'boolean');

export const typography: SettingField[] = [
  choice('family', fontFamilies, true), num('size', [6, 512]),
  num('weight', [100, 900], 100), field('color', 'color'),
  num('stroke', [0, 40]), field('strokeColor', 'color'),
  num('shadow', [0, 60]), field('shadowColor', 'color')
];

export const alertLook: SettingField[] = [
  field('title'), field('showMessage', 'boolean'),
  media('media', 'visual'), media('sound', 'sound'),
  num('volume', [0, 1], 0.05), seconds('duration', 0.1),
  seconds('textDelay'), choice('layout', layouts),
  choice('enter', animations), seconds('enterDuration', 0, 10),
  choice('exit', animations), seconds('exitDuration', 0, 10)
];

export const alertFields: SettingField[] = [
  field('enabled', 'boolean'), num('minAmount', [0, UNBOUNDED]), num('priority', [0, 100]), ...alertLook
];
export const listRule: SettingField[] = [field('enabled', 'boolean'), num('minAmount', [0, UNBOUNDED]), field('template')];
export const variationFields: SettingField[] = [choice('condition', conditions), field('value')];
export const variationLook = alertLook.filter((item) => ['title', 'media', 'sound'].includes(item.key));

export const creditHeaders: SettingField[] = ['follow', 'sub', 'gift_sub', 'cheer', 'raid', 'redemption', 'mod', 'vip', 'chatter']
  .map((key) => field(key, 'text', `heading.${key}`));
export const tierWeights: SettingField[] = tiers.map((tier) => num(tier, [0, 100], 1, `tier.${tier}`));

export const geometryFields: SettingField[] = [
  num('x', [0, 7680], 1, 'geometry.x'), num('y', [0, 7680], 1, 'geometry.y'),
  num('w', [1, 7680], 1, 'geometry.w'), num('h', [1, 7680], 1, 'geometry.h'),
  num('rotation', [-360, 360], 1, 'geometry.rotation'), num('opacity', [0, 1], 0.01, 'geometry.opacity')
];

export const widgetFields: Record<WidgetKind, SettingField[]> = {
  alertbox: [seconds('queueDelay', 0, 60), num('interruptThreshold', [0, 100]), thirdPartyEmotes],
  eventlist: [num('count', [1, UNBOUNDED], 1, 'events'), choice('direction', directions), seconds('fade')],
  goal: [
    choice('metric', metrics), num('start', [0, UNBOUNDED]), num('target', [1, UNBOUNDED]), num('manual', [0, UNBOUNDED]),
    field('title'), choice('display', ['horizontal', 'vertical', 'ring', 'text']),
    field('fill', 'color'), field('track', 'color')
  ],
  label: [choice('source', sources), field('template'), field('empty'), field('marquee', 'boolean')],
  chatbox: [
    num('count', [1, 100], 1, 'lines'), seconds('fade'), choice('direction', directions),
    field('hideCommands', 'boolean'), field('hideUsers', 'list'),
    field('badges', 'boolean'), field('nameColor', 'color'),
    field('nativeColors', 'boolean'), field('emotes', 'boolean'),
    choice('theme', ['clean', 'bubbles', 'boxed']), thirdPartyEmotes
  ],
  emotewall: [
    choice('motion', ['fly', 'rain', 'bounce']), num('max', [1, 200]),
    num('size', [8, 512], 1, 'emoteSize'), seconds('lifetime', 0.1, 60),
    num('combo', [1, 999]), thirdPartyEmotes
  ],
  timer: [choice('mode', ['countdown', 'countup', 'subathon']), field('at', 'datetime'), choice('format', clockFormats, true), field('endText')],
  credits: [
    num('speed', [1, 1000]), field('loop', 'boolean'),
    field('showMods', 'boolean'), field('showVips', 'boolean'), field('showChatters', 'boolean')
  ],
  text: [field('text', 'area'), choice('align', ['left', 'center', 'right'])],
  image: [media('media', 'image'), choice('fit', fit)],
  video: [media('media', 'video'), choice('fit', fit), field('loop', 'boolean'), field('muted', 'boolean')],
  slideshow: [media('images', 'image', 'media-list'), seconds('interval', 0.1), seconds('fade', 0, 30, 'crossfade'), choice('fit', fit)],
  custom: []
};

export const labelOf = (item: SettingField): string => item.label ?? item.key;
