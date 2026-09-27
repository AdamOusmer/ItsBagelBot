// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const MIN_POINT_VALUE = -9223372036854775808n;
const MAX_POINT_VALUE = 9223372036854775807n;

// Keep SQL BIGINT values as decimal text until arithmetic or display needs BigInt.
export function parsePointValue(raw: unknown): string | null {
  if (typeof raw === 'number') return Number.isSafeInteger(raw) ? String(raw) : null;
  if (typeof raw !== 'string') return null;
  const digits = raw.trim();
  if (!/^-?\d{1,19}$/.test(digits)) return null;
  const value = BigInt(digits);
  return value < MIN_POINT_VALUE || value > MAX_POINT_VALUE ? null : value.toString();
}

// Prefer the additive exact field. Reject rounded legacy numbers during rollout.
export function readPointBalance(raw: { points: unknown; points_exact?: unknown }): string {
  const source = raw.points_exact === undefined && typeof raw.points === 'number' ? raw.points : raw.points_exact;
  const value = parsePointValue(source);
  if (value === null) throw new Error('invalid point balance from loyalty');
  return value;
}

export function formatPointValue(raw: string, locale?: Intl.LocalesArgument): string {
  return BigInt(raw).toLocaleString(locale);
}
