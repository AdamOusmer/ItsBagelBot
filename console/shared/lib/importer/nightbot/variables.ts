// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Variable layer of the Nightbot parser: the token table and the translation
// loop. Scanning lives in ./scan, definition synthesis in ./fetchdefs.
//
// Decision record — Nightbot variable table (https://docs.nightbot.tv/
// commands/variables):
//
//	$(user) / $(touser) / $(channel) → {user} / {touser} / {channel}
//	$(query)                         → {args}
//	$(querystring)                   → literal + warn  (URL-encoded upstream)
//	$(1) $(2) …                      → literal + warn  (no per-word token here)
//	$(count)                         → literal + warn  (mutates upstream)
//	$(urlfetch URL) / $(customapi …) → {urlfetch:nightbot_<cmd>} + def
//	$(eval …) $(twitch …) $(time …)  → literal + warn  (no equivalent)
//
// $(querystring) is deliberately NOT folded onto {args}: it URL-encodes, and
// its whole reason to exist is being pasted inside a URL, where handing over
// raw args produces a different request rather than a lossy one. $(count)
// mutates (increments, then returns) while {counter:*} only reads, so
// translating it would silently drop the increment — a behavior change, not a
// translation.

import { parseFetchArgs } from './fetchdefs';
import type { FetchSlotSink } from './fetchdefs';
import { nextToken } from './scan';
import type { Token } from './scan';

// MAX_PASSES bounds the translation loop: pass 1 translates every leaf token,
// pass 2 sees composites whose interior now reads as plain text, and three
// settle any realistically nested response while guaranteeing termination.
const MAX_PASSES = 3;

export interface TranslationResult {
  text: string;
  warns: string[];
  jsonFetch: boolean;
}

interface TokenResult {
  repl: string;
  warned: boolean;
  jsonFetch?: boolean;
}

// SIMPLE_TOKENS maps a bare Nightbot variable onto its substitution token.
// Membership here means "no arguments, no sub-fields": a token whose body
// carries anything past the name is an attempt at something else and takes the
// literal+warn path with every other unmapped variable.
const SIMPLE_TOKENS: Record<string, string> = {
  user: '{user}',
  touser: '{touser}',
  channel: '{channel}',
  query: '{args}'
};

const FETCH_HEADS = new Set(['urlfetch', 'customapi']);

const literal = (token: Token): TokenResult => ({ repl: token.raw, warned: true });

// classify resolves one scanned token to its replacement. warned=true marks an
// attempted-but-unmappable variable; the caller reports one warn per distinct
// token.
function classify(token: Token, sink?: FetchSlotSink): TokenResult {
  if (token.head === '') return { repl: token.raw, warned: token.rest !== '' };
  const simple = SIMPLE_TOKENS[token.head];
  if (simple !== undefined) return token.rest === '' ? { repl: simple, warned: false } : literal(token);
  if (FETCH_HEADS.has(token.head)) return fetchToken(token, sink);
  // querystring, count, 1..N, eval, twitch, time, countdown, weather, …
  return literal(token);
}

// fetchToken extracts one urlfetch/customapi call into a synthesized
// definition. Extraction is safe by construction: the URL is copied byte-exact
// out of the response text — no fetch, no resolution, no key handling happens
// here — and the response keeps working at runtime through the reviewed,
// sandboxed definition instead of an unreviewed URL pasted into chat text.
// Without a sink (timers carry no command name to build a slug from) or with
// unusable arguments the token stays literal and warned.
function fetchToken(token: Token, sink?: FetchSlotSink): TokenResult {
  if (!sink) return literal(token);
  const args = parseFetchArgs(token.rest);
  if (!args) return literal(token);
  const key = sink.acquire(args.url);
  if (key === null) return literal(token);
  return { repl: `{urlfetch:${key}}`, warned: false, jsonFetch: args.json };
}

// Warnings collects the distinct tokens a translation could not map, in
// first-seen order, across every pass of one response.
class Warnings {
  private readonly seen = new Set<string>();
  readonly tokens: string[] = [];

  note(raw: string): void {
    if (this.seen.has(raw)) return;
    this.seen.add(raw);
    this.tokens.push(raw);
  }
}

// Pass is one left-to-right sweep's outcome, threaded back into the loop below.
interface Pass {
  text: string;
  changed: boolean;
  jsonFetch: boolean;
}

// translatePass rewrites every token visible in one sweep.
function translatePass(text: string, sink: FetchSlotSink | undefined, warns: Warnings): Pass {
  const pass: Pass = { text: '', changed: false, jsonFetch: false };
  let pos = 0;

  for (let token = nextToken(text, pos); token; token = nextToken(text, pos)) {
    const res = classify(token, sink);
    pass.text += text.slice(pos, token.start) + res.repl;
    pass.jsonFetch ||= res.jsonFetch === true;
    pass.changed ||= res.repl !== token.raw;
    pos = token.end;
    if (res.warned) warns.note(token.raw);
  }

  pass.text += text.slice(pos);
  return pass;
}

// translateVariables rewrites Nightbot variables into this bot's single-pass
// {key} substitution syntax, returning the text plus each distinct token that
// could not be mapped, in first-seen order.
export function translateVariables(inText: string, sink?: FetchSlotSink): TranslationResult {
  const warns = new Warnings();
  let text = inText;
  let jsonFetch = false;

  for (let n = 0; n < MAX_PASSES; n++) {
    const pass = translatePass(text, sink, warns);
    text = pass.text;
    jsonFetch ||= pass.jsonFetch;
    if (!pass.changed) break;
  }
  return { text, warns: warns.tokens, jsonFetch };
}
