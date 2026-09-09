// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The console-side mirror of the bot's pure token utilities:
// app/twitch/sesame/engine/scope/pure.go (the palette and {repeat:…}),
// math.go (the {math:…} evaluator) and clock.go (the countdown clock).
//
// These four — {math:…}, {queryescape:…}, {pathescape:…}, {repeat:n:phrase} —
// are functions of their own payload and nothing else, so the preview COMPUTES
// them rather than showing a stand-in: a rehearsal that prints "42" where chat
// will print "7" teaches the broadcaster the wrong thing, and a stand-in for
// {queryescape:…} would hide exactly the character the broadcaster is trying to
// get right. {countdown}/{countup} are not in that set (they read the clock)
// and the dice are not either (they draw), so both keep deterministic sample
// values in rehearsal.ts.
//
// The two implementations are pinned against ONE fixture,
// app/twitch/sesame/engine/scope/testdata/pure.golden.json, asserted here by
// pure.test.ts and in Go by TestPureGolden, so a grammar change that lands in
// only one language fails both suites rather than shipping a preview that lies.

import type { VarToken } from './tmpl';

/** Every name scope.Pure owns beyond the dice ({random}, {choice}, which live
 * with the module palette because module replies resolve them too). */
export const UTIL_NAMES: ReadonlySet<string> = new Set([
  'math',
  'queryescape',
  'pathescape',
  'repeat',
  'countdown',
  'countup'
]);

/** The subset whose value is a pure function of the payload, so the preview
 * can compute the real answer. The clock tokens are excluded on purpose. */
export const COMPUTED_UTILS: ReadonlySet<string> = new Set([
  'math',
  'queryescape',
  'pathescape',
  'repeat'
]);

/** Compute one deterministic utility from the span that named it. The caller
 * has already checked the name is in COMPUTED_UTILS and that the span carried
 * a payload; a payload the utility will not accept resolves to '' (so the
 * span's fallback renders), exactly as in Go. */
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

/** Resolve one span against the computed utilities, or null to leave it to
 * the caller (a name outside the set, or a utility span with no payload —
 * {math} names no expression, so it stays literal like any typo). */
export function resolveComputedUtil(token: VarToken): string | null {
  if (!COMPUTED_UTILS.has(token.name) || token.payload === null) return null;
  return computeUtil(token);
}

// --- {math:…} (engine/scope/math.go) --------------------------------------

/** Bytes {math:…} will look at, matching scope.MaxMathExpr. */
const MAX_MATH_EXPR = 64;

// int64 bounds. The evaluator works in BigInt rather than in JavaScript
// numbers so the preview agrees with Go bit for bit: a double loses precision
// past 2^53, so {math:9007199254740993} would print an even number here and
// the true one in chat. Every step is range-checked against these, and a step
// that leaves the range fails the whole expression, exactly as the Go
// overflow guards do.
const INT64_MIN = -(2n ** 63n);
const INT64_MAX = 2n ** 63n - 1n;

/**
 * Evaluate a small integer arithmetic expression, or return '' for anything
 * this will not evaluate: over the byte cap, a character outside
 * "0-9 + - * / ( )" and space, an unbalanced paren, a division by zero, or an
 * int64 overflow at any step.
 *
 * The grammar is a recursive descent, identical to math.go's:
 *
 *   sum     := product (('+' | '-') product)*
 *   product := unary (('*' | '/') unary)*
 *   unary   := '-' unary | primary
 *   primary := digits | '(' sum ')'
 */
export function evalMath(expr: string): string {
  if (byteLength(expr) > MAX_MATH_EXPR) return '';
  const s: MathState = { src: expr, pos: 0 };
  const val = sum(s);
  if (val === null || !atEnd(s)) return '';
  return val.toString();
}

/** The four operators the grammar folds. Spelling them as a union rather than
 * as bytes of a string is what keeps the two fold tables below and the
 * operator sets each level draws from in agreement: a fifth operator has to be
 * added here before either can mention it. */
type MathOp = '+' | '-' | '*' | '/';

const ADDITIVE_OPS: readonly MathOp[] = ['+', '-'];
const MULTIPLICATIVE_OPS: readonly MathOp[] = ['*', '/'];

/** Where the parser is in the expression it is reading.
 *
 * The rules below take it explicitly instead of hanging off a class, so this
 * file lines up function-for-function with math.go's recursive-descent parser
 * (sum, product, unary, primary, number). The two are pinned against one
 * golden fixture, and a rule that drifts in only one language is exactly what
 * that pinning exists to catch — which it can only do while the two are
 * readable side by side. */
interface MathState {
  readonly src: string;
  pos: number;
}

/** The next non-space character without consuming it, or '' at the end. */
function peek(s: MathState): string {
  while (s.pos < s.src.length && s.src[s.pos] === ' ') s.pos++;
  return s.pos < s.src.length ? s.src[s.pos] : '';
}

/** Only spaces remain. A parse that stops early ("1+2)", "1 2") failed. */
function atEnd(s: MathState): boolean {
  return peek(s) === '';
}

function sum(s: MathState): bigint | null {
  return binary(s, ADDITIVE_OPS, product, applyAdditive);
}

function product(s: MathState): bigint | null {
  return binary(s, MULTIPLICATIVE_OPS, unary, applyMultiplicative);
}

/** The one left-associative fold both levels are: parse an operand, then fold
 * each following operator drawn from ops. */
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

/** The operator at the cursor when it is one of ops, else null: the end of the
 * expression ('' from peek) matches no operator, so the fold above terminates
 * without a separate end check. */
function opAt(s: MathState, ops: readonly MathOp[]): MathOp | null {
  const c = peek(s);
  return ops.find((op) => op === c) ?? null;
}

/** A leading '-' run. A leading '+' is deliberately not accepted. */
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

/** A run of decimal digits. A run too large for an int64 fails the parse
 * rather than saturating: a silently clamped number is a wrong answer printed
 * with confidence. */
function number(s: MathState): bigint | null {
  peek(s); // skip leading spaces
  const start = s.pos;
  while (digitAt(s)) s.pos++;
  if (s.pos === start) return null;
  return fit(BigInt(s.src.slice(start, s.pos)));
}

/** A decimal digit sits at the cursor. Past the end of the expression reads as
 * undefined, which is not a digit, so the caller needs no separate bound. */
function digitAt(s: MathState): boolean {
  const c = s.src[s.pos];
  return c !== undefined && c >= '0' && c <= '9';
}

/** Keep a value inside the int64 range, or fail the expression. */
function fit(val: bigint): bigint | null {
  return val < INT64_MIN || val > INT64_MAX ? null : val;
}

function applyAdditive(op: MathOp, a: bigint, b: bigint): bigint | null {
  // Subtraction is negate-then-add, not a - b, so it fails on the one operand
  // Go's negate() refuses: int64's most negative value has no positive
  // counterpart, and "-1 - MIN" would otherwise print a number here that the
  // bot refuses to print in chat.
  if (op === '-') {
    if (b === INT64_MIN) return null;
    return fit(a - b);
  }
  return fit(a + b);
}

function applyMultiplicative(op: MathOp, a: bigint, b: bigint): bigint | null {
  if (op === '*') return fit(a * b);
  if (b === 0n) return null;
  // BigInt division truncates toward zero, like Go's integer division.
  return fit(a / b);
}

// --- {queryescape:…} / {pathescape:…} (net/url) ---------------------------

const encoder = new TextEncoder();

function byteLength(s: string): number {
  return encoder.encode(s).length;
}

/** Go's url.QueryEscape: everything outside the unreserved set is
 * percent-encoded from its UTF-8 bytes, and a space becomes '+'.
 *
 * Hand-rolled rather than encodeURIComponent, which is NOT the same function:
 * it leaves !'()* unescaped, encodes a space as %20, and would therefore build
 * a different URL than the one the bot requests. The whole point of the token
 * is that the request the preview shows is the request chat makes. */
export function queryEscape(text: string): string {
  return escapeBytes(text, QUERY_RULES);
}

/** Go's url.PathEscape (encodePathSegment): the unreserved set, plus the
 * reserved sub-delimiters a path SEGMENT may carry ($ & + : = @). The segment
 * separators (/ ; , ?) and everything else are percent-encoded, a space
 * included. */
export function pathEscape(text: string): string {
  return escapeBytes(text, PATH_RULES);
}

/** The two decisions Go's escape() takes, and the only two: which bytes pass
 * through as themselves, and what 0x20 becomes (null encodes it like any
 * other rejected byte). They travel as one value because picking one of them
 * from one escaper and one from the other is not a spelling either URL
 * grammar has. */
interface EscapeRules {
  readonly keep: (b: number) => boolean;
  readonly space: string | null;
}

const QUERY_RULES: EscapeRules = { keep: isUnreservedByte, space: '+' };
const PATH_RULES: EscapeRules = { keep: isPathSegmentByte, space: null };

/** Percent-encode every byte the rules reject, upper-case hex like Go's
 * escape(). */
function escapeBytes(text: string, rules: EscapeRules): string {
  let out = '';
  for (const b of encoder.encode(text)) {
    if (rules.keep(b)) out += String.fromCharCode(b);
    else if (b === 0x20 && rules.space !== null) out += rules.space;
    else out += '%' + b.toString(16).toUpperCase().padStart(2, '0');
  }
  return out;
}

/** RFC 3986 §2.3 unreserved: alphanumerics and - _ . ~ */
function isUnreservedByte(b: number): boolean {
  const c = String.fromCharCode(b);
  return /[A-Za-z0-9\-_.~]/.test(c);
}

/** The unreserved set plus the reserved characters Go leaves alone inside a
 * path segment. */
function isPathSegmentByte(b: number): boolean {
  return isUnreservedByte(b) || '$&+:=@'.includes(String.fromCharCode(b));
}

// --- {repeat:n:phrase} (engine/scope/pure.go) -----------------------------

/** Matching scope.MaxRepeatCount and scope.MaxRepeatBytes. Read the decision
 * record in pure.go: the caps are a Twitch line-length budget, not a trust
 * boundary — a command template is broadcaster-authored. */
const MAX_REPEAT_COUNT = 20;
const MAX_REPEAT_BYTES = 480;

/** Render "n:phrase" as the phrase n times, space separated, or '' when the
 * payload is not that shape or would blow a cap. */
export function repeatPhrase(payload: string): string {
  const colon = payload.indexOf(':');
  if (colon < 0) return '';
  const phrase = payload.slice(colon + 1);
  const n = repeatCount(payload.slice(0, colon));
  if (phrase === '' || n === null) return '';
  if (n * byteLength(phrase) + n - 1 > MAX_REPEAT_BYTES) return '';
  return Array(n).fill(phrase).join(' ');
}

/** The count, digits only: a sign is not part of a spelling anyone writes
 * here, and 0 or over the cap is refused so the span renders its fallback
 * instead of an empty run that looks like a lost phrase. */
function repeatCount(text: string): number | null {
  if (!/^[0-9]+$/.test(text)) return null;
  const n = Number(text);
  return n < 1 || n > MAX_REPEAT_COUNT ? null : n;
}
