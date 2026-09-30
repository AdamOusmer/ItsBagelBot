// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emit, normalizeInstant, positional, slice } from '../targets';
import { normalizeName } from '../validate';
import type { FetchSlotSink } from '../nightbot/fetchdefs';
import { scanTokens } from '../nightbot/scan';
import { FossabotExportError } from './envelope';
import type { Token } from '../nightbot/scan';
import { Warnings, TranslationOutput, fetchToken, literal, positionalToken, runPasses } from '../nightbot/variables';
import type { TokenResult } from '../nightbot/variables';

const MAX_REFERENCE_DEPTH = 3;

// These bound intermediate strings and actual translation work, before chat
// canonicalization. 64 Ki UTF-16 units leaves ample room for verbose source
// variables around the final 5 x 500-byte response. Work counts scanned and
// appended units, including every recursive branch and translation pass.
// The import budget allows 2000 ordinary commands without letting each command
// independently spend the full per-response budget on repeated references.
const MAX_EXPANDED_UNITS = 64 << 10;
const MAX_TRANSLATION_WORK = 1 << 20;
const MAX_TOKEN_VISITS = 8192;
const MAX_IMPORT_WORK = 64 << 20;

export class TranslationWorkBudget {
  private remaining = MAX_IMPORT_WORK;

  consume(units: number): void {
    if (units > this.remaining) throw limitError('the import translation work limit');
    this.remaining -= units;
  }
}

function limitError(limit: string): FossabotExportError {
  return new FossabotExportError(`importer/fossabot: response expansion exceeds ${limit}; simplify command references and try again`);
}

class ResponseBudget {
  private remainingWork = MAX_TRANSLATION_WORK;
  private remainingVisits = MAX_TOKEN_VISITS;

  constructor(private readonly shared: TranslationWorkBudget) {}

  private consume(units: number): void {
    if (units > this.remainingWork) throw limitError('the per-response translation work limit');
    this.shared.consume(units);
    this.remainingWork -= units;
  }

  scan(text: string): void {
    if (text.length > MAX_EXPANDED_UNITS) throw limitError('the 64 Ki-character intermediate response limit');
    this.consume(text.length);
  }

  visit(): void {
    if (this.remainingVisits === 0) throw limitError('the per-response token visit limit');
    this.remainingVisits--;
  }

  append(currentLength: number, fragment: string): void {
    if (fragment.length > MAX_EXPANDED_UNITS - currentLength)
      throw limitError('the 64 Ki-character intermediate response limit');
    this.consume(fragment.length);
  }
}

export interface TranslationResult {
  text: string;
  warns: string[];
}

export interface TranslationContext {
  sink?: FetchSlotSink;
  lookup?: (name: string) => string | null;
  workBudget?: TranslationWorkBudget;
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

function expandReferences(text: string, ctx: TranslationContext, depth: number, note: Note, budget: ResponseBudget): string {
  budget.scan(text);
  const out = new TranslationOutput(budget);
  let pos = 0;
  for (const token of scanTokens(text)) {
    budget.visit();
    out.append(text.slice(pos, token.start));
    out.append(expandOne(token, ctx, depth, note, budget));
    pos = token.end;
  }
  out.append(text.slice(pos));
  return out.text();
}

function expandOne(token: Token, ctx: TranslationContext, depth: number, note: Note, budget: ResponseBudget): string {
  if (token.head !== 'references') return token.raw;
  const target = referenceTarget(token, depth, ctx);
  if (target === null) {
    note(token.raw);
    return token.raw;
  }
  return expandReferences(target, ctx, depth + 1, note, budget);
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
  const budget = new ResponseBudget(ctx.workBudget ?? new TranslationWorkBudget());
  const expanded = expandReferences(inText, ctx, 0, note, budget);
  const { text } = runPasses(expanded, (token) => classify(token, ctx), warns, budget);
  return { text, warns: warns.tokens };
}
