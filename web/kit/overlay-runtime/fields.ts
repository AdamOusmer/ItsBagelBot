// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const animations = ['none', 'fade', 'slide-up', 'slide-down', 'slide-left', 'slide-right', 'zoom', 'pop', 'bounce', 'flip'] as const;
export const layouts = ['above', 'below', 'left', 'right', 'overlay'] as const;
export const conditions = ['amount_min', 'amount_exact', 'tier', 'months_min', 'count_min', 'message_contains', 'random'] as const;
export const metrics = [
  'session_follows',
  'session_subs',
  'sub_points',
  'session_bits',
  'all_time_follows',
  'all_time_subs',
  'all_time_bits',
  'manual'
] as const;
export const sources = [
  'latest_follow',
  'latest_sub',
  'latest_resub',
  'latest_cheer',
  'latest_raid',
  'latest_gift',
  'latest_redemption',
  'session_follows',
  'session_subs',
  'session_resubs',
  'session_gifts',
  'session_bits',
  'session_raids',
  'session_redemptions',
  'top_cheerer',
  'top_gifter',
  'all_time_top_cheerer',
  'all_time_top_gifter'
] as const;
export const tiers = ['1000', '2000', '3000', 'prime'] as const;
export const directions = ['newest_first', 'newest_last'] as const;
const fit = ['contain', 'cover', 'fill'] as const;
const MAX = Number.MAX_SAFE_INTEGER;

type FieldFormat = 'text' | 'family' | 'color' | 'media' | 'media-list' | 'list' | 'datetime' | 'boolean' | 'number' | 'select';
export type SettingField = {
  key: string;
  type: FieldFormat;
  options?: readonly string[];
  min?: number;
  max?: number;
};

const field = (key: string, type: FieldFormat = 'text'): SettingField => ({ key, type });
const num = (key: string, [min, max]: [number, number]): SettingField => ({ key, type: 'number', min, max });
const choice = (key: string, options: readonly string[]): SettingField => ({ key, type: 'select', options });

export const typography = [
  field('family', 'family'),
  num('size', [6, 512]),
  num('weight', [100, 900]),
  field('color', 'color'),
  num('stroke', [0, 40]),
  field('strokeColor', 'color'),
  num('shadow', [0, 60]),
  field('shadowColor', 'color')
];

export const alertLook = [
  field('title'),
  field('showMessage', 'boolean'),
  field('media', 'media'),
  field('sound', 'media'),
  num('volume', [0, 1]),
  num('duration', [0.1, 300]),
  num('textDelay', [0, 300]),
  choice('layout', layouts),
  choice('enter', animations),
  num('enterDuration', [0, 10]),
  choice('exit', animations),
  num('exitDuration', [0, 10])
];

export const alertFields = [field('enabled', 'boolean'), num('minAmount', [0, MAX]), num('priority', [0, 100]), ...alertLook];
export const listRule = [field('enabled', 'boolean'), num('minAmount', [0, MAX]), field('template')];
export const variationFields = [choice('condition', conditions), field('value')];
export const creditHeaders = ['follow', 'sub', 'gift_sub', 'cheer', 'raid', 'redemption'].map((key) => field(key));
export const tierWeights = tiers.map((tier) => num(tier, [0, 100]));

export const widgetFields = {
  alertbox: [num('queueDelay', [0, 60]), num('interruptThreshold', [0, 100])],
  eventlist: [num('count', [1, 10]), choice('direction', directions), num('fade', [0, 300])],
  goal: [
    choice('metric', metrics),
    num('start', [0, MAX]),
    num('target', [1, MAX]),
    num('manual', [0, MAX]),
    field('title'),
    choice('display', ['horizontal', 'vertical', 'ring', 'text']),
    field('fill', 'color'),
    field('track', 'color')
  ],
  label: [choice('source', sources), field('template'), field('empty'), field('marquee', 'boolean')],
  chatbox: [
    num('count', [1, 100]),
    num('fade', [0, 300]),
    choice('direction', directions),
    field('hideCommands', 'boolean'),
    field('hideUsers', 'list'),
    field('badges', 'boolean'),
    field('nameColor', 'color'),
    field('nativeColors', 'boolean'),
    field('emotes', 'boolean'),
    choice('theme', ['clean', 'bubbles', 'boxed'])
  ],
  emotewall: [
    choice('motion', ['fly', 'rain', 'bounce']),
    num('max', [1, 200]),
    num('size', [8, 512]),
    num('lifetime', [0.1, 60]),
    num('combo', [1, 999])
  ],
  timer: [
    choice('mode', ['countdown', 'countup', 'subathon']),
    field('at', 'datetime'),
    choice('format', ['hh:mm:ss', 'mm:ss', 'seconds']),
    field('endText')
  ],
  credits: [num('speed', [1, 1000]), field('loop', 'boolean')],
  text: [field('text'), choice('align', ['left', 'center', 'right'])],
  image: [field('media', 'media'), choice('fit', fit)],
  video: [field('media', 'media'), choice('fit', fit), field('loop', 'boolean'), field('muted', 'boolean')],
  slideshow: [field('images', 'media-list'), num('interval', [0.1, 300]), num('fade', [0, 30]), choice('fit', fit)],
  custom: [field('bundle', 'media')]
} satisfies Record<string, SettingField[]>;
