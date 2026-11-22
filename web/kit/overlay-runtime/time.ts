// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Subathon } from './feed';

export function remaining(state: Subathon, now = Date.now()): number {
  const seconds = state.running && state.end_at ? (Date.parse(state.end_at) - now) / 1000 : (state.remaining_seconds ?? 0);
  return Math.max(0, Math.ceil(seconds));
}

export function clock(seconds: number, format: 'hh:mm:ss' | 'mm:ss' | 'seconds' = 'hh:mm:ss'): string {
  if (format === 'seconds') return String(seconds);
  const ss = String(seconds % 60).padStart(2, '0');
  if (format === 'mm:ss') return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${ss}`;
  const mm = String(Math.floor(seconds / 60) % 60).padStart(2, '0');
  return `${String(Math.floor(seconds / 3600)).padStart(2, '0')}:${mm}:${ss}`;
}
