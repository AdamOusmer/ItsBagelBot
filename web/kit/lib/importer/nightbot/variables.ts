// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { emit, normalizeInstant, positional } from '../targets';
import { parseFetchArgs } from './fetchdefs';
import type { FetchSlotSink } from './fetchdefs';
import { scanTokens } from './scan';
import type { Token } from './scan';
import { twitchToken } from './twitch';

const MAX_PASSES = 3;

export interface TranslationResult {
  text: string;
  warns: string[];
  jsonFetch: boolean;
}

export interface TokenResult {
  repl: string;
  warned: boolean;
  jsonFetch?: boolean;
}

export const SIMPLE_TOKENS: Record<string, string> = {
  user: emit('user')!,
  touser: emit('touser')!,
  channel: emit('channel')!,
  query: emit('args')!,
  querystring: emit('querystring')!,
  count: emit('count')!
};

const FETCH_HEADS = new Set(['urlfetch', 'customapi']);

const POSITIONAL = /^([1-9]|[12][0-9]|30)$/;

export function positionalToken(token: Token): string | null {
  if (token.rest !== '' || !POSITIONAL.test(token.head)) return null;
  return positional(Number(token.head));
}

export const literal = (token: Token): TokenResult => ({ repl: token.raw, warned: true });

function timeToken(token: Token): TokenResult | null {
  if (token.head !== 'time' || token.rest.trim() === '') return null;
  const span = emit('time', token.rest.trim());
  return span === null ? literal(token) : { repl: span, warned: false };
}

function countdownSpan(head: 'countdown' | 'countup', raw: string): string | null {
  const normalized = normalizeInstant(raw);
  return normalized === null ? null : emit(head, normalized);
}

function countdownToken(token: Token): TokenResult | null {
  if (token.head !== 'countdown' && token.head !== 'countup') return null;
  const raw = token.rest.trim();
  if (raw === '') return literal(token);
  const span = countdownSpan(token.head as 'countdown' | 'countup', raw);
  return span === null ? literal(token) : { repl: span, warned: false };
}

function classify(token: Token, sink?: FetchSlotSink): TokenResult {
  if (token.head === '') return { repl: token.raw, warned: token.rest !== '' };
  const simple = SIMPLE_TOKENS[token.head];
  if (simple !== undefined) return token.rest === '' ? { repl: simple, warned: false } : literal(token);
  const word = positionalToken(token);
  if (word) return { repl: word, warned: false };
  if (FETCH_HEADS.has(token.head)) return fetchToken(token, sink);
  const special = specialToken(token);
  if (special) return special;
  return literal(token);
}

function specialToken(token: Token): TokenResult | null {
  return timeToken(token) ?? countdownToken(token) ?? twitchToken(token);
}

export function fetchToken(token: Token, sink?: FetchSlotSink): TokenResult {
  if (!sink) return literal(token);
  const args = parseFetchArgs(token.rest);
  if (!args) return literal(token);
  const key = sink.acquire(args.url);
  if (key === null) return literal(token);
  const span = emit('urlfetch', key);
  if (span === null) return literal(token);
  return { repl: span, warned: false, jsonFetch: args.json };
}

export class Warnings {
  private readonly seen = new Set<string>();
  readonly tokens: string[] = [];

  note(raw: string): void {
    if (this.seen.has(raw)) return;
    this.seen.add(raw);
    this.tokens.push(raw);
  }
}

interface Pass {
  text: string;
  changed: boolean;
  jsonFetch: boolean;
}

type Classify = (token: Token) => TokenResult;

// Optional work hooks let the Fossabot adapter bound its reference expansion
// and these shared translation passes with the same per-response/import budget.
export interface TranslationBudget {
  scan(text: string): void;
  visit(): void;
  append(currentLength: number, fragment: string): void;
}

// Retain each fragment once and join once per pass/branch. This keeps output
// construction linear even for many tiny tokens, and checks limits before
// retaining an unsafe fragment.
export class TranslationOutput {
  private readonly fragments: string[] = [];
  private length = 0;

  constructor(private readonly budget?: TranslationBudget) {}

  append(fragment: string): void {
    this.budget?.append(this.length, fragment);
    if (fragment.length === 0) return;
    this.fragments.push(fragment);
    this.length += fragment.length;
  }

  text(): string {
    return this.fragments.join('');
  }
}

function translatePass(text: string, classifyToken: Classify, warns: Warnings, budget?: TranslationBudget): Pass {
  budget?.scan(text);
  const out = new TranslationOutput(budget);
  const pass: Pass = { text: '', changed: false, jsonFetch: false };
  let pos = 0;

  for (const token of scanTokens(text)) {
    budget?.visit();
    const res = classifyToken(token);
    out.append(text.slice(pos, token.start));
    out.append(res.repl);
    pass.jsonFetch ||= res.jsonFetch === true;
    pass.changed ||= res.repl !== token.raw;
    pos = token.end;
    if (res.warned) warns.note(token.raw);
  }

  out.append(text.slice(pos));
  pass.text = out.text();
  return pass;
}

export function runPasses(
  inText: string,
  classifyToken: Classify,
  warns: Warnings,
  budget?: TranslationBudget
): { text: string; jsonFetch: boolean } {
  let text = inText;
  let jsonFetch = false;

  for (let n = 0; n < MAX_PASSES; n++) {
    const pass = translatePass(text, classifyToken, warns, budget);
    text = pass.text;
    jsonFetch ||= pass.jsonFetch;
    if (!pass.changed) break;
  }
  return { text, jsonFetch };
}

export function translateVariables(inText: string, sink?: FetchSlotSink): TranslationResult {
  const warns = new Warnings();
  const { text, jsonFetch } = runPasses(inText, (token) => classify(token, sink), warns);
  return { text, warns: warns.tokens, jsonFetch };
}
