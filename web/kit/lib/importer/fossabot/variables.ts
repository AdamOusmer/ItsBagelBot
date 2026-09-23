// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Variable layer of the Fossabot parser: reference expansion, the token table
// and the translation loop.
//
// Fossabot writes variables in the same $(…) syntax Nightbot does, so the
// scanner is literally the Nightbot one (../nightbot/scan) rather than a second
// copy of the same paren matching, and synthesized fetch definitions ride the
// same slot sink (../nightbot/fetchdefs). Only the table below is Fossabot's.
//
// Decision record: Fossabot variable table (docs.fossabot.com/variables,
// cross-checked against every token the live myth directory actually uses,
// extended phase 6 against the documented table for the bare/argument forms
// below the live directory never happened to exercise):
//
//	$(user) / $(sender)              → {user}     (both mean the caller)
//	$(user.id)                       → {user.id}
//	$(user.login)                    → {user.login}
//	$(user.followers)                → {followers}
//	$(touser)                        → {touser}
//	$(channel)                       → {channel}
//	$(uptime) $(title) $(game)       → {uptime} {title} {game}
//	$(followage) $(accountage)       → {followage} {accountage}
//	$(query)                         → {args}
//	$(1) $(2) … $(30)                → {1} {2} … {30}
//	$(chatters.random)               → {random.viewer}
//	$(chatters.count)                → {chatters}
//	$(count.get <name>)              → {counter:<name>}  (read-only)
//	$(randint <a> <b>)                → {random:<a>-<b>}
//	$(math <expr>)                   → {math:<expr>}
//	$(time)                          → {time}
//	$(countdown <date>)              → {countdown:<date>} when the date parses
//	$(repeat <n> <text>)             → {repeat:<n>:<text>}
//	$(indexN) / $(indexN <fb>)       → {N} / {N|<fb>}   (docs.fossabot.com/variables/indexes)
//	$(fromindexN) / $(fromindexN <fb>) → {N:} / {N:|<fb>} (…/fromindex)
//	$(customapi URL)                 → {urlfetch:fossabot_<cmd>} + definition
//	$(customapi)                     → literal + warn (no URL to extract)
//	$(references other)              → the referenced command's response, inlined
//	everything else                  → literal + warn
//
// The two $(user.…) subfields are NOT the same value as $(user): {user.id} is
// the stable platform id and {user.login} the lower-case login, while {user}
// is the display name, so folding either onto {user} would change what chat
// reads. The word-number family maps straight across (same 1-based meaning,
// same empty answer for a missing word); $(31) and up have no token here and
// keep the literal+warn path rather than translating into a span that would
// stay literal in chat.
//
// {points} ({points.name}, the loyalty balance and its currency name) gains
// no mapping: Fossabot's loyalty currency lives in a provider block outside
// the command language entirely, so there is no $(…) variable naming it to
// translate from, unlike $(followage)/$(accountage) above (which ARE plain
// variables the table documents).
//
// $(count.get <name>) is the ONE inbound counter spelling that means what
// {counter:<name>} means (a NAMED channel counter read); $(count.increment
// <name>) WRITES upstream and has no {…} equivalent — counter writes moved to
// the command's own "bump a counter" option instead of a response token, so
// there is no span for one to translate onto — and it stays literal+warn.
// Neither is the command's own use count
// ({count}, the {uses} alias) — Fossabot has no per-command usage variable at
// all, so nothing here maps onto bare {count}. (The Nightbot layer does map
// its own $(count), which IS that unnamed per-command variable; see
// ../nightbot/variables.)
//
// $(time) takes no timezone argument in Fossabot's own table — unlike
// Nightbot's/SLCB's $(time <tz>)-shaped calls, which stay literal+warn in
// their own layers for exactly that reason — so it maps cleanly onto this
// bot's {time}, which reads the broadcaster's own Local Time module
// configuration the same way.
//
// The conditional ({if:cond:then:else}) gains no mapping, and that is a
// checked answer rather than an omission: the table above is this file's
// record of Fossabot's variable language, cross-checked against the live myth
// directory, and it carries no conditional variable at all. There is no
// $(if …) to fold onto {if:…}, and nothing was invented from a guess at a
// spelling nobody has observed. A response that wanted one keeps the
// literal+warn path that sends it to review.
//
// The emote catalog ({emotes:7tv}, {emotes:bttv}, {emotes:ffz},
// {random.emote}) gains no mapping for the same checked reason: the table
// above carries no emote-list variable at all. Fossabot's own emote handling
// is a moderation setting, not something a response can print, so there is
// nothing here to fold onto these tokens and nothing was invented from a guess
// at a spelling.
//
// {random.chatter} (recent talkers, distinct from {random.viewer} above) gains
// no mapping either: the table carries $(chatters.random), which is the
// currently-connected-viewer list {random.viewer} already reads, and no
// separate recent-talkers variable to distinguish it from.
//
// $(user) and $(sender) both fold onto {user}: Fossabot's own docs describe
// them as the same person (sender is the older spelling), so keeping them apart
// would invent a distinction the source never had. $(count.increment …),
// $(setgame), $(nuke), $(rngphrase …), $(youtube …) and friends stay literal:
// each either mutates state, calls a Twitch API this bot exposes differently,
// or randomizes without a bound this bot's {random}/{choice} can express, and
// a wrong mapping is worse than visible untranslated text the broadcaster can
// fix in review.

import { emit, normalizeInstant, positional, slice } from '../targets';
import { normalizeName } from '../validate';
import { parseFetchArgs } from '../nightbot/fetchdefs';
import type { FetchSlotSink } from '../nightbot/fetchdefs';
import { nextToken } from '../nightbot/scan';
import type { Token } from '../nightbot/scan';
import { positionalToken } from '../nightbot/variables';

// MAX_PASSES bounds the translation loop, as in the Nightbot layer: pass 1
// translates every leaf token, pass 2 sees composites whose interior now reads
// as plain text, and three settle any realistic response while guaranteeing
// termination.
const MAX_PASSES = 3;

// MAX_REFERENCE_DEPTH bounds $(references …) inlining. Fossabot resolves a
// reference at runtime, we resolve it at import time, so a chain (a references
// b references c) has to stop somewhere and a cycle must not hang the browser.
// Three levels covers every chain observed in real directories (the deepest was
// two) and a reference past the cap stays literal with the usual warn.
const MAX_REFERENCE_DEPTH = 3;

export interface TranslationResult {
  text: string;
  warns: string[];
}

// TranslationContext carries what a command-level translation can reach:
// the slot sink that turns $(customapi …) into a definition, and the lookup
// that resolves $(references …) against the rest of the same feed. Both are
// optional so a caller with neither still gets literal+warn behaviour.
export interface TranslationContext {
  sink?: FetchSlotSink;
  lookup?: (name: string) => string | null;
}

// Note records one token that could not be mapped. The caller reports one
// warning per distinct token, so notes are deduplicated by raw text.
type Note = (raw: string) => void;

interface TokenResult {
  repl: string;
  warned: boolean;
}

// Exported so ../../variables/parity.test.ts can assert every emitted target
// head is a Variable head or alias, with no change to translation behaviour.
export const SIMPLE_TOKENS: Record<string, string> = {
  user: emit('user')!,
  sender: emit('user')!,
  touser: emit('touser')!,
  channel: emit('channel')!,
  // The two channel facts Fossabot spells as bare, argument-less variables.
  // Both are gated here by the !uptime / !title toggle on the Commands page,
  // which Fossabot has no equivalent of — the token stays visible in chat
  // while that command is off, which is what the guide and the chip hint say
  // and what the importer cannot say for the broadcaster.
  uptime: emit('uptime')!,
  title: emit('title')!,
  query: emit('args')!,
  // Phase 6 additions: bare, argument-less variables Fossabot's own table
  // documents the same way $(uptime)/$(title) already were above.
  game: emit('game')!,
  followage: emit('followage')!,
  accountage: emit('accountage')!,
  time: emit('time')!
};

// SUBFIELD_TOKENS maps a dotted Fossabot spelling, keyed by the whole body
// ("<head><rest>"), onto its token here. It is matched before SIMPLE_TOKENS so
// $(user.id) reads as a subfield rather than as $(user) with leftovers.
export const SUBFIELD_TOKENS: Record<string, string> = {
  'user.id': emit('user.id')!,
  'user.login': emit('user.login')!,
  // $(chatters.random) draws one name out of who is in chat right now —
  // {random.viewer}'s own definition, not {random.chatter} (recent talkers).
  'chatters.random': emit('random.viewer')!,
  'chatters.count': emit('chatters')!,
  'user.followers': emit('followers')!
};

const literal = (token: Token): TokenResult => ({ repl: token.raw, warned: true });

// COUNT_GET reads $(count.get <name>)'s counter name; count.increment (a
// write) has no equivalent and keeps the literal+warn path — see the
// decision record above this table.
const COUNT_GET = /^\.get\s+(.+)$/;

// countGetToken maps $(count.get <name>) onto the read-only {counter:<name>}
// this bot already has, or null when the subfield is not .get (count.increment,
// an unknown .subfield, or a bare $(count) — none of those are a read).
function countGetToken(token: Token): TokenResult | null {
  const m = COUNT_GET.exec(token.rest);
  if (!m) return null;
  const span = emit('counter', normalizeName(m[1].trim()));
  return span === null ? literal(token) : { repl: span, warned: false };
}

// RANDINT reads $(randint <a> <b>)'s two integer bounds.
const RANDINT = /^\s+(-?\d+)\s+(-?\d+)\s*$/;

function randintToken(token: Token): TokenResult | null {
  const m = RANDINT.exec(token.rest);
  if (!m) return null;
  // {random:a-b} does accept a negative bound (e.g. {random:-5--1}, already
  // pinned in streamelements.test.ts's own vector table), so a negative a or
  // b is not refused here — only the ordering needs fixing: $(randint 100 1)
  // is a real reversed call SLCB's own $randnum handles the same way
  // (boundedRange, streamlabs-desktop/parameters.ts), min/max rather than
  // literal a/b order, so the swap does not silently mint an empty range.
  const a = Number(m[1]);
  const b = Number(m[2]);
  const span = emit('random', `${Math.min(a, b)}-${Math.max(a, b)}`);
  return span === null ? literal(token) : { repl: span, warned: false };
}

// mathToken maps $(math <expr>) onto {math:<expr>} verbatim: the expression
// is evaluated by the SAME grammar on both sides (engine/pure.ts's evalMath
// mirrors scope.Pure's), so a payload this bot cannot evaluate resolves to ''
// in chat exactly as an unparseable one already does upstream — no separate
// validation is owed here.
function mathToken(token: Token): TokenResult | null {
  const expr = token.rest.trim();
  if (expr === '') return null;
  const span = emit('math', expr);
  return span === null ? literal(token) : { repl: span, warned: false };
}

// countdownToken maps $(countdown <date>) onto {countdown:<date>}, normalizing
// Fossabot's own free-form date spelling through targets.ts's normalizeInstant
// — the fixed grammar shared with streamlabs-desktop/parameters.ts's
// $countdown(d) and nightbot/variables.ts's $(countdown …), never Date.parse
// (see normalizeInstant's own decision record for why). A bare time with no
// calendar date is refused inside normalizeInstant itself (its TIME_ONLY
// guard); any other unparsable date stays literal and warns, since that is
// something the broadcaster can fix, unlike a token that would just render
// empty.
function countdownToken(token: Token): TokenResult | null {
  const raw = token.rest.trim();
  if (raw === '') return null;
  const normalized = normalizeInstant(raw);
  const span = normalized === null ? null : emit('countdown', normalized);
  return span === null ? literal(token) : { repl: span, warned: false };
}

// REPEAT reads $(repeat <n> <text>)'s count and phrase.
const REPEAT = /^\s+(\d+)\s+(.+)$/s;

function repeatToken(token: Token): TokenResult | null {
  const m = REPEAT.exec(token.rest);
  if (!m) return null;
  const span = emit('repeat', `${m[1]}:${m[2]}`);
  return span === null ? literal(token) : { repl: span, warned: false };
}

// INDEX_HEAD reads $(indexN)/$(fromindexN)'s word number, which Fossabot
// spells INSIDE the name (docs.fossabot.com/variables/indexes, /fromindex) —
// unlike every other source's positional family, where N is a separate
// argument. nextToken's NAME_RUN already includes digits, so "index2" scans
// as one head; this just reads the two shapes back out of it. The optional
// trailing word is the fallback: $(index2 everyone) -> {2|everyone},
// $(fromindex2 nothing) -> {2:|nothing}.
const INDEX_HEAD = /^(index|fromindex)(\d+)$/;

// indexToken maps $(indexN [fb]) onto {N}/{N|fb} and $(fromindexN [fb]) onto
// {N:}/{N:|fb}, or null when the head is not one of these two shapes.
//
// Decision record: a missing argument with NO fallback renders differently
// on the two sides. Fossabot posts the literal text
// "[Error: Index 2 is not in arguments.]" in that case; this bot's positional
// word just resolves to empty. That is a genuine behavioural difference
// between the platforms, not a translation gap this importer can close (there
// is no {…} spelling for "print an error string when empty"), so it is
// recorded here rather than warned about at import time: the response TEXT
// translates correctly either way, only what a broadcaster sees for a
// genuinely missing word differs.
function indexToken(token: Token): TokenResult | null {
  const m = INDEX_HEAD.exec(token.head);
  if (!m) return null;
  const n = Number(m[2]);
  // '' -> undefined: positional()/slice() treat a missing fallback argument
  // and an explicit undefined identically (no fallback minted), which is
  // what an empty trailing word means here — no nested ternary needed once
  // that mapping is made once, up front.
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

// classify resolves one scanned token to its replacement. warned=true marks an
// attempted-but-unmappable variable.
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

// fetchToken extracts one $(customapi …) call into a synthesized definition.
// Extraction is safe by construction: the URL is copied byte-exact out of the
// response text (nothing is fetched or resolved here) and the response keeps
// working at runtime through the reviewed, sandboxed definition instead of an
// unreviewed URL pasted into chat text. A bare $(customapi) carries no URL to
// extract at all (the live myth directory has one, in its !song command, where
// Fossabot fills the URL in elsewhere), so it stays literal and warns.
function fetchToken(token: Token, sink?: FetchSlotSink): TokenResult {
  if (!sink) return literal(token);
  const args = parseFetchArgs(token.rest);
  if (!args) return literal(token);
  const key = sink.acquire(args.url);
  if (key === null) return literal(token);
  const span = emit('urlfetch', key);
  if (span === null) return literal(token);
  return { repl: span, warned: false };
}

// expandReferences inlines $(references other) with the referenced command's
// RAW response before any token is translated, so the inlined text goes through
// the same table as text written in place. Fossabot resolves the reference on
// every call; this bot has no such indirection, so the choice is inline it or
// lose it, and inlining preserves what chat actually saw.
function expandReferences(text: string, ctx: TranslationContext, depth: number, note: Note): string {
  let out = '';
  let pos = 0;
  for (let token = nextToken(text, pos); token; token = nextToken(text, pos)) {
    out += text.slice(pos, token.start) + expandOne(token, ctx, depth, note);
    pos = token.end;
  }
  return out + text.slice(pos);
}

// expandOne resolves one token if it is a reference this feed can satisfy;
// anything else (another variable, an unknown target, a chain past the depth
// cap) comes back unchanged for the translation loop to deal with.
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

// Pass is one left-to-right sweep's outcome, threaded back into the loop below.
interface Pass {
  text: string;
  changed: boolean;
}

function translatePass(text: string, ctx: TranslationContext, note: Note): Pass {
  const pass: Pass = { text: '', changed: false };
  let pos = 0;

  for (let token = nextToken(text, pos); token; token = nextToken(text, pos)) {
    const res = classify(token, ctx);
    pass.text += text.slice(pos, token.start) + res.repl;
    pass.changed ||= res.repl !== token.raw;
    pos = token.end;
    if (res.warned) note(token.raw);
  }

  pass.text += text.slice(pos);
  return pass;
}

// translateVariables rewrites Fossabot variables into this bot's single-pass
// {key} substitution syntax, returning the text plus each distinct token that
// could not be mapped, in first-seen order.
export function translateVariables(
  inText: string,
  ctx: TranslationContext = {}
): TranslationResult {
  const warns: string[] = [];
  const seen = new Set<string>();
  const note: Note = (raw) => {
    if (seen.has(raw)) return;
    seen.add(raw);
    warns.push(raw);
  };

  let text = expandReferences(inText, ctx, 0, note);
  for (let n = 0; n < MAX_PASSES; n++) {
    const pass = translatePass(text, ctx, note);
    text = pass.text;
    if (!pass.changed) break;
  }
  return { text, warns };
}
