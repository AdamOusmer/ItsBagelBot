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
// cross-checked against every token the live myth directory actually uses):
//
//	$(user) / $(sender)              → {user}     (both mean the caller)
//	$(user.id)                       → {userid}
//	$(user.login)                    → {user.login}
//	$(touser)                        → {touser}
//	$(channel)                       → {channel}
//	$(uptime)                        → {uptime}
//	$(title)                         → {title}
//	$(query)                         → {args}
//	$(1) $(2) … $(30)                → {1} {2} … {30}
//	$(customapi URL)                 → {urlfetch:fossabot_<cmd>} + definition
//	$(customapi)                     → literal + warn (no URL to extract)
//	$(references other)              → the referenced command's response, inlined
//	everything else                  → literal + warn
//
// The two $(user.…) subfields are the only dotted spellings with a token on
// this side, and they are NOT the same value as $(user): {userid} is the
// stable platform id and {user.login} the lower-case login, while {user} is
// the display name, so folding either onto {user} would change what chat
// reads. The word-number family maps straight across (same 1-based meaning,
// same empty answer for a missing word); $(31) and up have no token here and
// keep the literal+warn path rather than translating into a span that would
// stay literal in chat.
//
// The viewer lookups this bot grew ({followage}, {accountage}, {points}) have
// no counterpart in the table above and gain none here: Fossabot spells follow
// age as $(twitch <channel> <user> "…"), a format-string call whose interior is
// a template of its own rather than a variable, and its points live in a
// provider block outside the command language. Translating a format string
// into a bare token would drop everything the broadcaster wrote around it, so
// both keep the literal+warn path.
//
// The command's own use count ({uses}) gains no mapping, and that is a
// checked answer rather than an omission: the table above is this file's
// record of Fossabot's variable language, and the only counter spellings in it
// are $(count.get <name>) and $(count.increment <name>) — both NAMED channel
// counters, neither one "how many times this command has run". Fossabot has no
// per-command usage variable to map, so nothing here translates into {uses}.
// (The Nightbot layer does map its $(count), which is exactly that unnamed
// per-command variable; see ../nightbot/variables.)
//
// The module facts ({quote}, {time}, {song}) and the read-only counter
// ({count:<name>}) gain no mapping for the same reason plus one of their own.
// $(count.get <name>) is the one inbound variable that means what {count:…}
// means, and it is NOT in the live directories this table was cross-checked
// against — mapping a spelling nothing observed uses would be a guess at
// Fossabot's counter scoping (per channel? per command?) that would show up as
// a wrong number in chat rather than as a warning in review. $(time <tz>)
// takes a per-call timezone where {time} reads the broadcaster's Local Time
// module, and Fossabot has no now-playing variable at all. All keep the
// literal+warn path; a mapping can be added the day a real directory carries
// one to check against.
//
// The conditional ({if:cond:then:else}) gains no mapping, and that is a
// checked answer rather than an omission: the table above is this file's
// record of Fossabot's variable language, cross-checked against the live myth
// directory, and it carries no conditional variable at all. There is no
// $(if …) to fold onto {if:…}, and nothing was invented from a guess at a
// spelling nobody has observed. A response that wanted one keeps the
// literal+warn path that sends it to review.
//
// The emote catalog ({7tvemotes}, {bttvemotes}, {ffzemotes},
// {random.emote}) gains no mapping for the same checked reason: the table
// above carries no emote-list variable at all. Fossabot's own emote handling
// is a moderation setting, not something a response can print, so there is
// nothing here to fold onto these tokens and nothing was invented from a guess
// at a spelling.
//
// The chat room ({chatters}, {random.chatter}) gains no mapping either, and
// for the plainest reason of all: the table above is this file's record of
// what Fossabot's variable language contains, and it carries no chatter count
// and no random-viewer variable to map. Nothing here was rewritten from a
// guess at what the source might spell them; the day one is observed in a
// real directory it can be added and checked, and until then a response that
// wanted one keeps the literal+warn path that sends it to review.
//
// The channel facts are the exception this table just gained, and only where
// it already documented the inbound spelling: $(uptime) and $(title) are named
// above, they take no argument, and this bot's {uptime} / {title} read the very
// same two facts through the same Twitch endpoints — so a translated response
// says in chat exactly what the Fossabot one said. Their offline behaviour even
// matches: both bots answer an offline channel's title and leave its uptime
// empty.
//
// {game} and {channel.viewers} get no inbound mapping, and that asymmetry is
// the point: the table above is this file's record of Fossabot's variable
// language, it lists neither a category nor a viewer-count variable, and
// inventing $(game) or $(stream.title) from a guess at what the source might
// spell them is exactly the class of mistake this file exists to avoid. The day
// one is observed in a real directory it can be added and checked against it.
//
// $(user) and $(sender) both fold onto {user}: Fossabot's own docs describe
// them as the same person (sender is the older spelling), so keeping them apart
// would invent a distinction the source never had. $(count.increment …),
// $(time …), $(setgame), $(nuke), $(rngphrase …), $(youtube …) and friends stay
// literal: each either mutates state, calls a Twitch API this bot exposes
// differently, or randomizes, and a wrong mapping is worse than visible
// untranslated text the broadcaster can fix in review.

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

const SIMPLE_TOKENS: Record<string, string> = {
  user: '{user}',
  sender: '{user}',
  touser: '{touser}',
  channel: '{channel}',
  // The two channel facts Fossabot spells as bare, argument-less variables.
  // Both are gated here by the !uptime / !title toggle on the Commands page,
  // which Fossabot has no equivalent of — the token stays visible in chat
  // while that command is off, which is what the guide and the chip hint say
  // and what the importer cannot say for the broadcaster.
  uptime: '{uptime}',
  title: '{title}',
  query: '{args}'
};

// SUBFIELD_TOKENS maps a dotted Fossabot spelling, keyed by the whole body
// ("<head><rest>"), onto its token here. It is matched before SIMPLE_TOKENS so
// $(user.id) reads as a subfield rather than as $(user) with leftovers.
const SUBFIELD_TOKENS: Record<string, string> = {
  'user.id': '{userid}',
  'user.login': '{user.login}'
};

const literal = (token: Token): TokenResult => ({ repl: token.raw, warned: true });

// classify resolves one scanned token to its replacement. warned=true marks an
// attempted-but-unmappable variable.
function classify(token: Token, ctx: TranslationContext): TokenResult {
  if (token.head === '') return { repl: token.raw, warned: token.rest !== '' };
  const mapped = SUBFIELD_TOKENS[token.head + token.rest] ?? positionalToken(token);
  if (mapped) return { repl: mapped, warned: false };
  const simple = SIMPLE_TOKENS[token.head];
  if (simple !== undefined) return token.rest === '' ? { repl: simple, warned: false } : literal(token);
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
  return { repl: `{urlfetch:${key}}`, warned: false };
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
