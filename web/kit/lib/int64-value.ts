// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MIN_INT64 = -9223372036854775808n;
export const MAX_INT64 = 9223372036854775807n;
const SIGNED = /^-?\d{1,19}$/;
const UNSIGNED = /^\d{1,19}$/;

function fromNumber(raw: number, min: bigint): string | null {
  return Number.isSafeInteger(raw) && raw >= min ? String(raw) : null;
}

function fromDecimal(raw: string, min: bigint): string | null {
  const digits = raw.trim();
  if (!(min < 0n ? SIGNED : UNSIGNED).test(digits)) return null;
  const value = BigInt(digits);
  return value < min || value > MAX_INT64 ? null : value.toString();
}

// JSON numbers above 2^53 lose digits. SQL BIGINT values travel as decimal
// strings; safe legacy numbers are accepted while services roll forward.
export function parseInt64(raw: unknown, min: bigint = MIN_INT64): string | null {
  if (typeof raw === 'number') return fromNumber(raw, min);
  if (typeof raw === 'string') return fromDecimal(raw, min);
  return null;
}

export function formatInt64(raw: string, locale?: Intl.LocalesArgument): string {
  return BigInt(raw).toLocaleString(locale);
}
