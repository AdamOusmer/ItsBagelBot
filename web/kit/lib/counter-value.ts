// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { formatInt64, MAX_INT64, parseInt64 } from './int64-value';

export const MAX_COUNTER_VALUE = MAX_INT64;

export function parseCounterValue(raw: unknown): string | null {
  return parseInt64(raw, 0n);
}

export function isCounterValue(raw: unknown): raw is string {
  return typeof raw === 'string' && parseCounterValue(raw) !== null;
}

export const formatCounterValue = formatInt64;
