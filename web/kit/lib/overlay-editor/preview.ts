// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Settings } from './defaults';

const SAMPLE_SECONDS = 300;
const SECONDS_PER_MINUTE = 60;
const MINUTES_PER_HOUR = 60;
const SECONDS_PER_HOUR = SECONDS_PER_MINUTE * MINUTES_PER_HOUR;

const pad = (value: number): string => String(value).padStart(2, '0');

export function sampleClock(format: unknown): string {
  if (format === 'seconds') return String(SAMPLE_SECONDS);
  const minutes = Math.floor(SAMPLE_SECONDS / SECONDS_PER_MINUTE);
  if (format === 'mm:ss') return `${pad(minutes)}:${pad(SAMPLE_SECONDS % SECONDS_PER_MINUTE)}`;
  return `${pad(Math.floor(SAMPLE_SECONDS / SECONDS_PER_HOUR))}:${pad(minutes % MINUTES_PER_HOUR)}:${pad(SAMPLE_SECONDS % SECONDS_PER_MINUTE)}`;
}

const text = (value: unknown): string | undefined => (typeof value === 'string' ? value : undefined);

export function previewText(kind: string, settings: Settings): string | undefined {
  if (kind === 'text') return text(settings.text);
  if (kind === 'label') return text(settings.template);
  if (kind === 'goal') return text(settings.title);
  if (kind === 'timer') return sampleClock(settings.format);
  return undefined;
}

export function previewMedia(kind: string, settings: Settings): string | undefined {
  if (kind === 'image' || kind === 'video') return text(settings.media) || undefined;
  if (kind !== 'slideshow') return undefined;
  const [first] = Array.isArray(settings.images) ? settings.images : [];
  return text(first) || undefined;
}
