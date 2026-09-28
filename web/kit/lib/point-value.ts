// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { formatInt64, parseInt64 } from './int64-value';

export function parsePointValue(raw: unknown): string | null {
  return parseInt64(raw);
}

// Prefer the additive exact field. Reject rounded legacy numbers during rollout.
export function readPointBalance(raw: { points: unknown; points_exact?: unknown }): string {
  const source = raw.points_exact === undefined && typeof raw.points === 'number' ? raw.points : raw.points_exact;
  const value = parsePointValue(source);
  if (value === null) throw new Error('invalid point balance from loyalty');
  return value;
}

export const formatPointValue: (raw: string, locale: Intl.LocalesArgument) => string = formatInt64;
