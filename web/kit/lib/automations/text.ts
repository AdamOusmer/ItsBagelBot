// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const SPACE_RANGES: readonly (readonly [number, number])[] = [
  [0x9, 0xd],
  [0x20, 0x20],
  [0x85, 0x85],
  [0xa0, 0xa0],
  [0x1680, 0x1680],
  [0x2000, 0x200a],
  [0x2028, 0x2029],
  [0x202f, 0x202f],
  [0x205f, 0x205f],
  [0x3000, 0x3000],
];
const CONTROL = /^\p{Cc}$/u;
const PRINTABLE = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]$/u;
const INT64_TEXT = /^[+-]?\d+$/;
const INT64_MAX = 2n ** 63n - 1n;
const INT64_MIN = -(2n ** 63n);
const encoder = new TextEncoder();

export type Text = string;
export type Char = string;
export type CharTest = (ch: Char) => boolean;

export function runeCount(s: Text): number {
  let n = 0;
  for (const _ of s) n++;
  return n;
}

function isGoSpace(ch: Char): boolean {
  const code = ch.codePointAt(0) ?? 0;
  return SPACE_RANGES.some(([lo, hi]) => code >= lo && code <= hi);
}

export function trimSpace(s: Text): Text {
  const chars = [...s];
  let start = 0;
  let end = chars.length;
  while (start < end && isGoSpace(chars[start])) start++;
  while (end > start && isGoSpace(chars[end - 1])) end--;
  return chars.slice(start, end).join('');
}

export function isCommandChar(ch: Char): boolean {
  return !isGoSpace(ch) && !CONTROL.test(ch);
}

export function isPrintChar(ch: Char): boolean {
  return PRINTABLE.test(ch);
}

export function isNameChar(ch: Char): boolean {
  return !CONTROL.test(ch);
}

export function isCounterChar(ch: Char): boolean {
  return ch !== ':' && !isGoSpace(ch);
}

export function allChars(s: Text, ok: CharTest): boolean {
  for (const ch of s) if (!ok(ch)) return false;
  return true;
}

export function parsesAsInt64(s: Text): boolean {
  if (!INT64_TEXT.test(s)) return false;
  const n = BigInt(s);
  return n >= INT64_MIN && n <= INT64_MAX;
}

export function compareBytes(a: Text, b: Text): number {
  const x = encoder.encode(a);
  const y = encoder.encode(b);
  const n = Math.min(x.length, y.length);
  for (let i = 0; i < n; i++) if (x[i] !== y[i]) return x[i] - y[i];
  return x.length - y.length;
}

export function byteLength(s: Text): number {
  return encoder.encode(s).length;
}
