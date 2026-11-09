// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface StatGlyph {
  key: string;
  text: string;
  phase: number | null;
}

export function digitPhase(position: number, place: number, window = 1): number {
  const divisor = 10 ** place;
  const digit = Math.floor(position / divisor) % 10;
  const carry = Math.max(0, position % divisor - (divisor - 1));
  return digit + (window > 0 ? Math.max(0, (carry - (1 - window)) / window) : 0);
}

export function statGlyphs(
  value: number,
  formatter: Intl.NumberFormat,
  numerals: string[],
  rate = Infinity
): StatGlyph[] {
  const parts = formatter.formatToParts(value);
  const digits = parts.filter((part) => part.type === 'integer' || part.type === 'fraction');
  const significand = Number(digits.map((part) =>
    (part.type === 'fraction' ? '.' : '') + Array.from(part.value).map((glyph) => numerals.indexOf(glyph)).join('')
  ).join(''));
  const fraction = parts.find((part) => part.type === 'fraction')?.value.length ?? 0;
  const compact = parts.some((part) => part.type === 'compact');
  const scale = compact && significand > 0 ? 10 ** Math.round(Math.log10(value / significand)) : 1;
  const position = value / scale * 10 ** fraction;
  const window = Math.min(1, rate * 0.14 * 10 ** fraction / scale);
  let place = digits.reduce((count, part) => count + Array.from(part.value).length, 0);
  return parts.flatMap<StatGlyph>((part, index) => {
    if (part.type !== 'integer' && part.type !== 'fraction') {
      return [{ key: `${part.type}-${index}`, text: part.value, phase: null }];
    }
    return Array.from(part.value, (text) => ({
      key: `digit-${--place}`, text, phase: digitPhase(position, place, window)
    }));
  });
}
