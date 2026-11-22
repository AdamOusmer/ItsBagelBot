// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AlertKind, Tier } from './feed';

export type Animation = 'none' | 'fade' | 'slide-up' | 'slide-down' | 'slide-left' | 'slide-right' | 'zoom' | 'pop' | 'bounce' | 'flip';
export type FontStyle = {
  family: string;
  size: number;
  weight: number;
  color: string;
  stroke: number;
  strokeColor: string;
  shadow: number;
  shadowColor: string;
};
export type AlertLayout = 'above' | 'below' | 'left' | 'right' | 'overlay';
export type AlertLook = {
  title: string;
  showMessage: boolean;
  media: string;
  sound: string;
  volume: number;
  duration: number;
  textDelay: number;
  layout: AlertLayout;
  enter: Animation;
  exit: Animation;
  enterDuration: number;
  exitDuration: number;
  style: FontStyle;
};
export type Condition = 'amount_min' | 'amount_exact' | 'tier' | 'months_min' | 'count_min' | 'message_contains' | 'random';
export type AlertVariation = {
  condition: Condition;
  value: string;
  overrides: Partial<Omit<AlertLook, 'style'>> & { style?: Partial<FontStyle> };
};
export type AlertConfig = AlertLook & { enabled: boolean; minAmount: number; priority: number; variations: AlertVariation[] };
export type Direction = 'newest_first' | 'newest_last';
export type Metric =
  | 'session_follows'
  | 'session_subs'
  | 'sub_points'
  | 'session_bits'
  | 'all_time_follows'
  | 'all_time_subs'
  | 'all_time_bits'
  | 'manual';
export type LabelSource =
  | 'latest_follow'
  | 'latest_sub'
  | 'latest_resub'
  | 'latest_cheer'
  | 'latest_raid'
  | 'latest_gift'
  | 'latest_redemption'
  | 'session_follows'
  | 'session_subs'
  | 'session_resubs'
  | 'session_gifts'
  | 'session_bits'
  | 'session_raids'
  | 'session_redemptions'
  | 'top_cheerer'
  | 'top_gifter'
  | 'all_time_top_cheerer'
  | 'all_time_top_gifter';
export type CustomField = {
  key: string;
  label: string;
  type: 'text' | 'number' | 'boolean' | 'color' | 'select';
  default: string | number | boolean;
  options?: string[];
};
export type CustomValue = CustomField['default'];
type Fit = 'contain' | 'cover' | 'fill';
export type WidgetSettings = {
  alertbox: { events: Record<AlertKind, AlertConfig>; queueDelay: number; interruptThreshold: number };
  eventlist: {
    count: number;
    direction: Direction;
    rules: Record<AlertKind, { enabled: boolean; minAmount: number; template: string }>;
    fade: number;
    style: FontStyle;
  };
  goal: {
    metric: Metric;
    start: number;
    target: number;
    manual: number;
    title: string;
    display: 'horizontal' | 'vertical' | 'ring' | 'text';
    fill: string;
    track: string;
    style: FontStyle;
    weights: Record<Exclude<Tier, ''>, number>;
  };
  label: { source: LabelSource; template: string; empty: string; marquee: boolean; style: FontStyle };
  chatbox: {
    count: number;
    fade: number;
    direction: Direction;
    hideCommands: boolean;
    hideUsers: string[];
    badges: boolean;
    nameColor: string;
    nativeColors: boolean;
    emotes: boolean;
    theme: 'clean' | 'bubbles' | 'boxed';
    style: FontStyle;
  };
  emotewall: { motion: 'fly' | 'rain' | 'bounce'; max: number; size: number; lifetime: number; combo: number; style: FontStyle };
  timer: {
    mode: 'countdown' | 'countup' | 'subathon';
    at: string;
    format: 'hh:mm:ss' | 'mm:ss' | 'seconds';
    endText: string;
    style: FontStyle;
  };
  credits: {
    headers: Record<'follow' | 'sub' | 'gift_sub' | 'cheer' | 'raid' | 'redemption', string>;
    speed: number;
    loop: boolean;
    style: FontStyle;
  };
  text: { text: string; align: 'left' | 'center' | 'right'; style: FontStyle };
  image: { media: string; fit: Fit };
  video: { media: string; fit: Fit; loop: boolean; muted: boolean };
  slideshow: { images: string[]; interval: number; fade: number; fit: Fit };
  custom: { bundle: string; values: Record<string, CustomValue> };
};
export type WidgetKind = keyof WidgetSettings;
export type WidgetBase = {
  id: string;
  name: string;
  x: number;
  y: number;
  w: number;
  h: number;
  rotation: number;
  opacity: number;
  hidden: boolean;
  locked: boolean;
};
export type Widget = { [K in WidgetKind]: WidgetBase & { kind: K; settings: WidgetSettings[K] } }[WidgetKind];
