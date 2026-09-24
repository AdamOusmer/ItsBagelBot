// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { VarToken } from './tmpl';

export const UTIL_NAMES: ReadonlySet<string> = new Set([
  'math',
  'queryescape',
  'pathescape',
  'repeat',
  'countdown',
  'countup'
]);

export const COMPUTED_UTILS: ReadonlySet<string> = new Set([
  'math',
  'queryescape',
  'pathescape',
  'repeat'
]);

export function computeUtil(token: VarToken): string {
  const payload = token.payload ?? '';
  switch (token.name) {
    case 'math':
      return evalMath(payload);
    case 'queryescape':
      return queryEscape(payload);
    case 'pathescape':
      return pathEscape(payload);
    default:
      return repeatPhrase(payload);
  }
}

export function resolveComputedUtil(token: VarToken): string | null {
  if (!COMPUTED_UTILS.has(token.name) || token.payload === null) return null;
  return computeUtil(token);
}

const MAX_MATH_EXPR_BYTES = 64;

const INT64_MIN = -(2n ** 63n);
const INT64_MAX = 2n ** 63n - 1n;

export function evalMath(expr: string): string {
  if (byteLength(expr) > MAX_MATH_EXPR_BYTES) return '';
  const s: MathState = { src: expr, pos: 0 };
  const val = sum(s);
  if (val === null || !atEnd(s)) return '';
  return val.toString();
}

type MathOp = '+' | '-' | '*' | '/';

const ADDITIVE_OPS: readonly MathOp[] = ['+', '-'];
const MULTIPLICATIVE_OPS: readonly MathOp[] = ['*', '/'];

interface MathState {
  readonly src: string;
  pos: number;
}

function peek(s: MathState): string {
  while (s.pos < s.src.length && s.src[s.pos] === ' ') s.pos++;
  return s.pos < s.src.length ? s.src[s.pos] : '';
}

function atEnd(s: MathState): boolean {
  return peek(s) === '';
}

function sum(s: MathState): bigint | null {
  return binary(s, ADDITIVE_OPS, product, applyAdditive);
}

function product(s: MathState): bigint | null {
  return binary(s, MULTIPLICATIVE_OPS, unary, applyMultiplicative);
}

function binary(
  s: MathState,
  ops: readonly MathOp[],
  operand: (s: MathState) => bigint | null,
  apply: (op: MathOp, a: bigint, b: bigint) => bigint | null
): bigint | null {
  let val = operand(s);
  if (val === null) return null;
  for (;;) {
    const op = opAt(s, ops);
    if (op === null) return val;
    s.pos++;
    const rhs = operand(s);
    if (rhs === null) return null;
    val = apply(op, val, rhs);
    if (val === null) return null;
  }
}

function opAt(s: MathState, ops: readonly MathOp[]): MathOp | null {
  const c = peek(s);
  return ops.find((op) => op === c) ?? null;
}

function unary(s: MathState): bigint | null {
  if (peek(s) !== '-') return primary(s);
  s.pos++;
  const val = unary(s);
  return val === null ? null : fit(-val);
}

function primary(s: MathState): bigint | null {
  if (peek(s) !== '(') return number(s);
  s.pos++;
  const val = sum(s);
  if (val === null || peek(s) !== ')') return null;
  s.pos++;
  return val;
}

function number(s: MathState): bigint | null {
  peek(s);
  const start = s.pos;
  while (digitAt(s)) s.pos++;
  if (s.pos === start) return null;
  return fit(BigInt(s.src.slice(start, s.pos)));
}

function digitAt(s: MathState): boolean {
  const c = s.src[s.pos];
  return c !== undefined && c >= '0' && c <= '9';
}

function fit(val: bigint): bigint | null {
  return val < INT64_MIN || val > INT64_MAX ? null : val;
}

function applyAdditive(op: MathOp, a: bigint, b: bigint): bigint | null {
  if (op === '-') {
    if (b === INT64_MIN) return null;
    return fit(a - b);
  }
  return fit(a + b);
}

function applyMultiplicative(op: MathOp, a: bigint, b: bigint): bigint | null {
  if (op === '*') return fit(a * b);
  if (b === 0n) return null;
  return fit(a / b);
}

const encoder = new TextEncoder();

function byteLength(s: string): number {
  return encoder.encode(s).length;
}

export function queryEscape(text: string): string {
  return escapeBytes(text, QUERY_RULES);
}

export function pathEscape(text: string): string {
  return escapeBytes(text, PATH_RULES);
}

interface EscapeRules {
  readonly keep: (b: number) => boolean;
  readonly space: string | null;
}

const QUERY_RULES: EscapeRules = { keep: isUnreservedByte, space: '+' };
const PATH_RULES: EscapeRules = { keep: isPathSegmentByte, space: null };

function escapeBytes(text: string, rules: EscapeRules): string {
  let out = '';
  for (const b of encoder.encode(text)) {
    if (rules.keep(b)) out += String.fromCharCode(b);
    else if (b === 0x20 && rules.space !== null) out += rules.space;
    else out += '%' + b.toString(16).toUpperCase().padStart(2, '0');
  }
  return out;
}

function isUnreservedByte(b: number): boolean {
  const c = String.fromCharCode(b);
  return /[A-Za-z0-9\-_.~]/.test(c);
}

function isPathSegmentByte(b: number): boolean {
  return isUnreservedByte(b) || '$&+:=@'.includes(String.fromCharCode(b));
}

const MAX_REPEAT_COUNT = 20;
const MAX_REPEAT_BYTES = 480;

export function repeatPhrase(payload: string): string {
  const colon = payload.indexOf(':');
  if (colon < 0) return '';
  const phrase = payload.slice(colon + 1);
  const n = repeatCount(payload.slice(0, colon));
  if (phrase === '' || n === null) return '';
  if (n * byteLength(phrase) + n - 1 > MAX_REPEAT_BYTES) return '';
  return Array(n).fill(phrase).join(' ');
}

function repeatCount(text: string): number | null {
  if (!/^[0-9]+$/.test(text)) return null;
  const n = Number(text);
  return n < 1 || n > MAX_REPEAT_COUNT ? null : n;
}
