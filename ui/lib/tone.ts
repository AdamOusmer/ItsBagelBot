// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Tone = 'neutral' | 'accent' | 'warm' | 'success' | 'warning' | 'danger' | 'info';

export type TagTone =
  | Extract<Tone, 'neutral' | 'success' | 'warning' | 'danger' | 'info'>
  | 'quiet' | 'live' | 'alpha' | 'pre' | 'incoming' | 'bare' | 'free' | 'paid' | 'vip' | 'banned' | 'inactive';
export type BadgeTone = Exclude<TagTone, 'info'>;
export type TextTone =
  | Extract<Tone, 'accent' | 'success' | 'danger' | 'warning'>
  | 'default' | 'muted' | 'muted-light' | 'muted-soft' | 'soft' | 'pale';
export type StatusDotTone = Extract<Tone, 'neutral' | 'success' | 'warning' | 'danger'>;
export type ProgressTone = StatusDotTone;
export type CodeTone = Extract<Tone, 'success' | 'danger'>;
export type StatTone = Extract<Tone, 'accent' | 'success'>;
export type SwitchRowTone = Extract<Tone, 'warning'>;
export type CardTone = Extract<Tone, 'accent' | 'warm'>;
export type SeriesTone = CardTone;
export type ToastKind = Extract<Tone, 'success' | 'danger' | 'info'>;
