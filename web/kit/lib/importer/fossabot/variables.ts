// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emit, normalizeInstant, positional, slice } from '../targets';
import { normalizeName } from '../validate';
import type { FetchSlotSink } from '../nightbot/fetchdefs';
import { nextToken } from '../nightbot/scan';
import type { Token } from '../nightbot/scan';
import { Warnings, fetchToken, literal, positionalToken, runPasses } from '../nightbot/variables';
import type { TokenResult } from '../nightbot/variables';

const MAX_REFERENCE_DEPTH = 3;

export interface TranslationResult {
  text: string;
  warns: string[];
}

export interface TranslationContext {
  sink?: FetchSlotSink;
  lookup?: (name: string) => string | null;
}

type Note = (raw: string) => void;

export const SIMPLE_TOKENS: Record<string, string> = {
  user: emit('user')!,
  sender: emit('user')!,
  touser: emit('touser')!,
  channel: emit('channel')!,
  uptime: emit('uptime')!,
  title: emit('title')!,
  query: emit('args')!,
  game: emit('game')!,
  followage: emit('followage')!,
  accountage: emit('accountage')!,
  time: emit('time')!
};

export const SUBFIELD_TOKENS: Record<string, string> = {
  'user.id': emit('user.id')!,
  'user.login': emit('user.login')!,
  'chatters.random': emit('random.viewer')!,
  'chatters.count': emit('chatters')!,
  'user.followers': emit('followers')!
};

const COUNT_GET = /^\.get\s+(.+)$/;

function countGetToken(token: Token): TokenResult | null {
  const m = COUNT_GET.exec(token.rest);
  if (!m) return null;
  const span = emit('counter', normalizeName(m[1].trim()));
  return span === null ? literal(token) : { repl: span, warned: false };
}

const RANDINT = /^\s+(-?\d+)\s+(-?\d+)\s*$/;

function randintToken(token: Token): TokenResult | null {
  const m = RANDINT.exec(token.rest);
  if (!m) return null;
  const a = Number(m[1]);
  const b = Number(m[2]);
  const span = emit('random', `${Math.min(a, b)}-${Math.max(a, b)}`);
  return span === null ? literal(token) : { repl: span, warned: false };
}

function mathToken(token: Token): TokenResult | null {
  const expr = token.rest.trim();
  if (expr === '') return null;
  const span = emit('math', expr);
  return span === null ? literal(token) : { repl: span, warned: false };
}

function countdownToken(token: Token): TokenResult | null {
  const raw = token.rest.trim();
  if (raw === '') return null;
  const normalized = normalizeInstant(raw);
  const span = normalized === null ? null : emit('countdown', normalized);
  return span === null ? literal(token) : { repl: span, warned: false };
}

const REPEAT = /^\s+(\d+)\s+(.+)$/s;

function repeatToken(token: Token): TokenResult | null {
  const m = REPEAT.exec(token.rest);
  if (!m) return null;
  const span = emit('repeat', `${m[1]}:${m[2]}`);
  return span === null ? literal(token) : { repl: span, warned: false };
}

const INDEX_HEAD = /^(index|fromindex)(\d+)$/;

function indexToken(token: Token): TokenResult | null {
  const m = INDEX_HEAD.exec(token.head);
  if (!m) return null;
  const n = Number(m[2]);
  const fallback = token.rest.trim() || undefined;
  const span = m[1] === 'index' ? positional(n, fallback) : slice(n, undefined, fallback);
  return span === null ? literal(token) : { repl: span, warned: false };
}

const HEAD_HANDLERS: Record<string, (token: Token) => TokenResult | null> = {
  count: countGetToken,
  randint: randintToken,
  math: mathToken,
  countdown: countdownToken,
  repeat: repeatToken
};

function classify(token: Token, ctx: TranslationContext): TokenResult {
  if (token.head === '') return { repl: token.raw, warned: token.rest !== '' };
  const mapped = SUBFIELD_TOKENS[token.head + token.rest] ?? positionalToken(token);
  if (mapped) return { repl: mapped, warned: false };
  const simple = SIMPLE_TOKENS[token.head];
  if (simple !== undefined) return token.rest === '' ? { repl: simple, warned: false } : literal(token);
  const handled = HEAD_HANDLERS[token.head]?.(token) ?? indexToken(token);
  if (handled) return handled;
  if (token.head === 'customapi') return fetchToken(token, ctx.sink);
  return literal(token);
}

function expandReferences(text: string, ctx: TranslationContext, depth: number, note: Note): string {
  let out = '';
  let pos = 0;
  for (let token = nextToken(text, pos); token; token = nextToken(text, pos)) {
    out += text.slice(pos, token.start) + expandOne(token, ctx, depth, note);
    pos = token.end;
  }
  return out + text.slice(pos);
}

function expandOne(token: Token, ctx: TranslationContext, depth: number, note: Note): string {
  if (token.head !== 'references') return token.raw;
  const target = referenceTarget(token, depth, ctx);
  if (target === null) {
    note(token.raw);
    return token.raw;
  }
  return expandReferences(target, ctx, depth + 1, note);
}

function referenceTarget(token: Token, depth: number, ctx: TranslationContext): string | null {
  if (depth >= MAX_REFERENCE_DEPTH) return null;
  const name = token.rest.trim();
  return name === '' ? null : (ctx.lookup?.(name) ?? null);
}

export function translateVariables(
  inText: string,
  ctx: TranslationContext = {}
): TranslationResult {
  const warns = new Warnings();
  const note: Note = (raw) => warns.note(raw);
  const expanded = expandReferences(inText, ctx, 0, note);
  const { text } = runPasses(expanded, (token) => classify(token, ctx), warns);
  return { text, warns: warns.tokens };
}
