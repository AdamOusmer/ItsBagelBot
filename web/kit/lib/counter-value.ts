// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MAX_COUNTER_VALUE = 9223372036854775807n;

function fromNumber(raw: number): string | null {
  if (!Number.isSafeInteger(raw)) return null;
  return raw < 0 ? null : String(raw);
}

function fromDecimal(raw: string): string | null {
  const digits = raw.trim();
  if (!/^\d{1,19}$/.test(digits)) return null;
  const value = BigInt(digits);
  return value > MAX_COUNTER_VALUE ? null : value.toString();
}

// JSON numbers above 2^53 lose digits. Counter RPC replies carry decimal
// strings; safe legacy numbers are accepted while services roll forward.
export function parseCounterValue(raw: unknown): string | null {
  if (typeof raw === 'number') return fromNumber(raw);
  if (typeof raw === 'string') return fromDecimal(raw);
  return null;
}

export function isCounterValue(raw: unknown): raw is string {
  return typeof raw === 'string' && parseCounterValue(raw) !== null;
}

export function formatCounterValue(raw: string, locale?: Intl.LocalesArgument): string {
  return BigInt(raw).toLocaleString(locale);
}
