// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Settings } from './defaults';

export type Bucket = 'events' | 'rules';
export type OverrideGroup = 'look' | 'style';
export type Variation = { condition: string; value: string; overrides: Settings & { style?: Settings } };

export const asSettings = (value: unknown): Settings => (typeof value === 'object' && value !== null ? (value as Settings) : {});

export const bucketOf = (kind: 'alertbox' | 'eventlist'): Bucket => (kind === 'alertbox' ? 'events' : 'rules');

export function entryOf(settings: Settings, bucket: Bucket, event: string): Settings {
  return asSettings(asSettings(settings[bucket])[event]);
}

export function patchEntry(settings: Settings, bucket: Bucket, event: string, patch: Settings): Settings {
  const bucketValue = asSettings(settings[bucket]);
  return { ...settings, [bucket]: { ...bucketValue, [event]: { ...asSettings(bucketValue[event]), ...patch } } };
}

export const variationsOf = (entry: Settings): Variation[] => (Array.isArray(entry.variations) ? (entry.variations as Variation[]) : []);

export function replaceVariation(list: Variation[], index: number, item: Variation): Variation[] {
  return list.map((entry, position) => (position === index ? item : entry));
}

export function withOverride(item: Variation, group: OverrideGroup, key: string, next: unknown): Variation {
  if (group === 'style') return { ...item, overrides: { ...item.overrides, style: { ...item.overrides.style, [key]: next } } };
  return { ...item, overrides: { ...item.overrides, [key]: next } };
}

export function overrideValue(item: Variation, group: OverrideGroup, key: string): unknown {
  return group === 'style' ? item.overrides.style?.[key] : item.overrides[key];
}

export const newVariation = (): Variation => ({ condition: 'random', value: '', overrides: {} });
