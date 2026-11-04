// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const LOOKAROUND = /\(\?<?[=!]/;
const BACKREFERENCE = /\\[1-9]|\\k</;
const GO_ONLY_ESCAPE_REJECTED = /\\[CFGHIJKLMNORTUVXYZcegijklmoquy]/g;
const FLAG_GROUP = /\(\?[imsU]+\)/g;
const FLAG_SCOPE = /\(\?[imsU-]+:/g;
const NAMED_GROUP = /\(\?P</g;
const REPEAT_COUNT = /\{(\d+)(?:,(\d*))?\}/y;
const MAX_REPEAT = 1000;

const UNSUPPORTED = [LOOKAROUND, BACKREFERENCE];

type RegexSource = string;

type Cursor = { source: RegexSource; at: number };

type ScanKind = 'open' | 'close' | 'atom' | 'modifier' | 'repeat';

type Scan = { end: number; kind: ScanKind; factor: number };

const SINGLE_CHARS: Readonly<Record<string, ScanKind>> = { '(': 'open', ')': 'close', '*': 'modifier', '+': 'modifier', '?': 'modifier' };

const atomUntil = (end: number): Scan => ({ end, kind: 'atom', factor: 1 });

function classEnd({ source, at }: Cursor): number {
  let i = at + 1;
  if (source[i] === '^') i++;
  if (source[i] === ']') i++;
  while (i < source.length && source[i] !== ']') i += source[i] === '\\' ? 2 : 1;
  return i + 1;
}

function repeatFactor(m: RegExpExecArray): number {
  return Math.max(Number(m[2] || m[1]), 1);
}

function repeatAt({ source, at }: Cursor): Scan | null {
  REPEAT_COUNT.lastIndex = at;
  const m = REPEAT_COUNT.exec(source);
  return m && { end: REPEAT_COUNT.lastIndex, kind: 'repeat', factor: repeatFactor(m) };
}

const SCANNERS: Readonly<Record<string, (c: Cursor) => Scan>> = {
  '\\': (c) => atomUntil(c.at + 2),
  '[': (c) => atomUntil(classEnd(c)),
  '{': (c) => repeatAt(c) ?? atomUntil(c.at + 1)
};

function scanAt(c: Cursor): Scan {
  const char = c.source[c.at]!;
  if (Object.hasOwn(SCANNERS, char)) return SCANNERS[char]!(c);
  return { end: c.at + 1, kind: Object.hasOwn(SINGLE_CHARS, char) ? SINGLE_CHARS[char]! : 'atom', factor: 1 };
}

type RepeatDepth = { parents: number[]; level: number; atom: number };

const DEPTH_STEPS: Readonly<Record<ScanKind, (d: RepeatDepth, scan: Scan) => void>> = {
  open: (d) => {
    d.parents.push(d.level);
    d.level = 1;
  },
  close: (d) => {
    d.atom = d.level;
    d.level = Math.max(d.parents.pop() ?? 1, d.atom);
  },
  atom: (d) => {
    d.atom = 1;
  },
  modifier: () => {},
  repeat: (d, scan) => {
    d.atom *= scan.factor;
    d.level = Math.max(d.level, d.atom);
  }
};

// Go rejects a repeat whose product with every enclosing repeat exceeds 1000.
function repeatTooLarge(source: RegexSource): boolean {
  const depth: RepeatDepth = { parents: [], level: 1, atom: 1 };
  for (let at = 0; at < source.length && depth.atom <= MAX_REPEAT; ) {
    const scan = scanAt({ source, at });
    at = scan.end;
    DEPTH_STEPS[scan.kind](depth, scan);
  }
  return depth.atom > MAX_REPEAT;
}

function hasGoRejectedEscape(source: RegexSource): boolean {
  return source.replace(/\\\\/g, '').search(GO_ONLY_ESCAPE_REJECTED) >= 0;
}

function hasUnsupportedSyntax(source: RegexSource): boolean {
  return UNSUPPORTED.some((syntax) => syntax.test(source)) || hasGoRejectedEscape(source);
}

function toJsSyntax(source: RegexSource): RegexSource {
  return source.replace(FLAG_GROUP, '').replace(FLAG_SCOPE, '(?:').replace(NAMED_GROUP, '(?<');
}

export function compilesAsRe2(pattern: RegexSource): boolean {
  if (hasUnsupportedSyntax(pattern) || repeatTooLarge(pattern)) return false;
  try {
    new RegExp(toJsSyntax(pattern));
    return true;
  } catch {
    return false;
  }
}
