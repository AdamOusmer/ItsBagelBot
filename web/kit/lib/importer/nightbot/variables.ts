// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Variable layer of the Nightbot parser: the token table and the translation
// loop. Scanning lives in ./scan, definition synthesis in ./fetchdefs.
//
// Decision record: Nightbot variable table (https://docs.nightbot.tv/
// commands/variables):
//
//	$(user) / $(touser) / $(channel) → {user} / {touser} / {channel}
//	$(query)                         → {args}
//	$(querystring)                   → {querystring}
//	$(1) $(2) … $(30)                → {1} {2} … {30}
//	$(count)                         → {count} (bare; the {uses} alias)
//	$(time <tz>)                     → {time:<tz>}  (phase 6; payload passed through unread)
//	$(countdown <date>) / $(countup <date>) → {countdown:<date>} / {countup:<date>}  (phase 6; date normalized, see countdownToken)
//	$(twitch <channel> "<fmt>")      → substitutes {{title}}/{{game}}/{{uptimeLength}}/
//	                                    {{viewers}}/{{followers}}/{{subscriberCount}} inside
//	                                    <fmt>, keeps surrounding text, warns per unmapped field
//	                                    (phase 6; see twitchToken)
//	$(urlfetch URL) / $(customapi …) → {urlfetch:nightbot_<cmd>} + def
//	$(eval …)      → literal + warn (runs JS we will never execute)
//	$(userlevel)   → literal + warn (no role token answers "what tier is this viewer")
//	$(weather …)   → literal + warn (needs a provider key we do not hold)
//
// The word-number family maps straight across: both sides mean "the n'th word
// typed after the trigger", both are 1-based, and both leave a missing word
// empty, so nothing about a translated response changes. Past {30} (this bot's
// cap, scope.MaxPositional) there is no token to map onto, so $(31) keeps the
// literal+warn path rather than translating into a span that would itself stay
// literal in chat — a warning the broadcaster sees beats a brace they discover
// live.
//
// $(querystring) maps onto {querystring} and is still deliberately NOT folded
// onto {args}: it URL-encodes, and its whole reason to exist is being pasted
// inside a URL, where handing over raw args produces a different request
// rather than a lossy one. Both sides encode the query component the same way
// (space to '+', everything outside the unreserved set percent-encoded), so a
// translated response builds the byte-identical request.
//
// $(count) maps onto bare {count} (uses.go's canonical spelling of {uses}),
// and that mapping replaced an earlier literal+warn path — the record of why
// is worth keeping, because the objection that killed the first two
// candidates does not apply to the third. Note the payload split: bare
// {count} is this mapping's target, never {count:<name>} — that spelling is
// scope.Store's counter READ (store.go), a completely different family that
// happens to share a head with this one (see rehearsal.ts's SampleScope.owns
// for how the two are told apart).
//
//   - {counter:<name>}: rejected. Nightbot's $(count) is per-command and
//     UNNAMED, ours is a named channel counter, so the translation would have
//     to invent a name and silently bind the imported command to a bucket the
//     broadcaster never chose.
//   - {count:<name>}: rejected for the same invented name — it is a counter
//     read, not a use count, whatever the command's bump_counter option (if
//     any) happens to be named.
//   - bare {count}: what both of those were reaching for. $(count) means "how
//     many times this command has run"; {count} (= {uses}) means the same
//     thing, per command, with no name to invent, because this bot counts
//     every custom command's runs on its own whether or not the response
//     prints the number.
//
// Two divergences ride along and are deliberate. The count starts from THIS
// bot's history, so a freshly imported command prints a small number where
// Nightbot printed a large one; there is no way to carry the old total over,
// since the import reads a response text and not a counter value. And {count}
// excludes the run doing the printing where $(count) includes it, so the first
// line after an import reads one lower than the same line did yesterday. Both
// are off-by-a-number, not off-by-a-meaning, which is why this is a mapping
// and not a warning: a broadcaster reading "hugged {count} times" gets the
// sentence they wrote, and a warning here would fire on every imported counter
// command while telling them nothing they could act on.
//
// {followage} gains a mapping (phase 6, twitchToken): $(twitch $(touser)
// "…{{followed}}…") is the SAME format-string call {{title}}/{{game}}/etc
// already map through above — {{followed}} could join TWITCH_FIELDS the day
// a real followage sample is checked against it, but is not in that table yet
// because nothing here has cross-checked its exact field name or shape
// against a live export. {accountage} and {points} still gain none:
// Nightbot's loyalty lives outside the command language entirely (no
// variable names it at all), and there is no account-age field in the
// documented {{…}} set either.
// {song} (the now-playing module fact) gains no mapping for the same reason
// $(twitch … "{{song}}") never joined TWITCH_FIELDS: Nightbot's song
// variable, if it has one, has not been checked against a real export, and
// guessing its field name is exactly the class of mistake this table exists
// to avoid.
// The conditional ({if:cond:then:else}) gains no mapping either, and it is
// the most tempting one to invent: Nightbot writes a conditional as
// $(eval …), a JavaScript expression evaluated over the other variables, and
// $(eval) is already in the table above on the literal+warn path. It stays
// there. Mapping it would mean parsing JavaScript and re-emitting whatever
// subset of it {if:…} can express (one variable, empty-or-equals, two literal
// texts), so every expression outside that subset would translate into a
// conditional that tests something else and STILL looks like it worked. A
// warning the broadcaster reads beats a reply that quietly says the wrong
// half. Nightbot has no non-eval conditional variable to map instead.
// The emote catalog ({emotes:7tv}, {emotes:bttv}, {emotes:ffz},
// {random.emote}) has none either: the table at the top of this file is the
// record of Nightbot's variable language, and it carries no emote-list
// variable. A response that wanted one keeps the literal+warn path that sends
// it to review, which is the honest answer for a spelling nobody has observed.
// The chat room ({chatters}, {random.chatter}) has no inbound counterpart at
// all: the table at the top of this file is the record of what Nightbot's
// variable language contains, and it carries neither a chatter count nor a
// random-viewer variable. Nothing was invented from a guess at what the source
// might spell them; a mapping can be added the day one is observed and can be
// checked against a real directory.
// The channel facts ({uptime}, {title}, {game}, {channel.viewers},
// {followers}, {subs}) DO gain a mapping (phase 6, twitchToken above): unlike
// the earlier decision to leave $(twitch …) as a whole literal+warn (Nightbot
// has no BARE $(uptime)/$(title) — every one of these is spelled inside a
// $(twitch <channel> "{{field}}") format-string call), the format string is
// now parsed rather than treated as an opaque template: the surrounding
// sentence is kept exactly as written, and only the {{field}} placeholders
// this bot recognizes are substituted — a rewrite of the fact, not of the
// broadcaster's sentence around it. title/game/uptimeLength additionally
// support the OTHER-channel form ($(twitch bob "{{title}}") -> {title:bob}),
// matching the `{head:<channel>}` payload variables.ts's catalog documents
// for exactly those three; viewers/followers/subscriberCount have no such
// form and stay unmapped when asked of another channel.
//
// $(countdown …)/$(countup …) also gain a mapping (phase 6, countdownToken):
// Nightbot's free-form date string ("Dec 25 2026 12:00:00 PST") is normalized
// through targets.ts's normalizeInstant (the same RFC3339/date-only-or-
// Date.parse path streamlabs-desktop/parameters.ts's $countdown(d) uses)
// rather than assumed to read; a date it cannot normalize, or a bare
// time-of-day with no calendar date at all (which Date.parse would silently
// anchor to today's date on whatever machine runs it, answering a different
// instant than Nightbot's own clock meant), stays literal and warns.

import { emit, normalizeInstant, positional } from '../targets';
import { parseFetchArgs } from './fetchdefs';
import type { FetchSlotSink } from './fetchdefs';
import { nextToken } from './scan';
import type { Token } from './scan';
import { twitchToken } from './twitch';

// MAX_PASSES bounds the translation loop: pass 1 translates every leaf token,
// pass 2 sees composites whose interior now reads as plain text, and three
// settle any realistically nested response while guaranteeing termination.
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

// SIMPLE_TOKENS maps a bare Nightbot variable onto its substitution token.
// Membership here means "no arguments, no sub-fields": a token whose body
// carries anything past the name is an attempt at something else and takes the
// literal+warn path with every other unmapped variable.
// Exported so ../../variables/parity.test.ts can assert every emitted target
// head is a Variable head or alias, with no change to translation behaviour.
export const SIMPLE_TOKENS: Record<string, string> = {
  user: emit('user')!,
  touser: emit('touser')!,
  channel: emit('channel')!,
  query: emit('args')!,
  querystring: emit('querystring')!,
  count: emit('count')!
};

const FETCH_HEADS = new Set(['urlfetch', 'customapi']);

// POSITIONAL matches the word numbers this bot has a token for: 1 to 30
// (scope.MaxPositional). Anchored, so $(1x) and $(031) are not word numbers
// here either.
const POSITIONAL = /^([1-9]|[12][0-9]|30)$/;

// positionalToken translates $(n) into "{n}", or returns null when the token
// is not a word number this bot can spell. Shared with the Fossabot layer,
// which writes the same family in the same syntax.
// The head becomes a span through targets.ts's positional(), the same
// round-tripped minting every other emitted token goes through: POSITIONAL
// already proves it is one or two digits, and positional()'s own range check
// (1..POSITIONAL_MAX) keeps that proof next to the emission instead of two
// screens above it.
export function positionalToken(token: Token): string | null {
  if (token.rest !== '' || !POSITIONAL.test(token.head)) return null;
  return positional(Number(token.head));
}

export const literal = (token: Token): TokenResult => ({ repl: token.raw, warned: true });

// timeToken maps $(time <tz>) onto {time:<tz>}, payload passed through
// unread: our resolver accepts IANA zone names, cities and regions, and UTC
// offsets (scope's Places table), so "America/New_York" or "Paris" both
// answer; an abbreviation like "EST" is not specially detected or rejected
// here — the minted {time:<tz>} token itself renders empty and falls back
// when the resolver cannot place it, exactly like any other unresolvable
// payload, so there is nothing this importer needs to validate ahead of time.
function timeToken(token: Token): TokenResult | null {
  if (token.head !== 'time' || token.rest.trim() === '') return null;
  const span = emit('time', token.rest.trim());
  return span === null ? literal(token) : { repl: span, warned: false };
}

// countdownToken maps $(countdown <date>)/$(countup <date>) onto
// {countdown:<date>}/{countup:<date>}, normalizing Nightbot's own free-form
// date spelling ("Dec 25 2015 12:00:00 AM EST") through targets.ts's
// normalizeInstant — the SAME fixed grammar (RFC3339 / YYYY-MM-DD / a
// month-name or MM/DD/YYYY date with a required zone), shared with
// streamlabs-desktop/parameters.ts's $countdown(d) and fossabot/variables.ts's
// $(countdown …), rather than three near-duplicate date readers. A time-only
// payload with no calendar date ("5:00:00 PM EST") is refused inside
// normalizeInstant itself (its own TIME_ONLY guard, also exported from
// targets.ts): it would otherwise answer a DIFFERENT calendar day than
// Nightbot's own clock read it against, and a warning the broadcaster can act
// on beats a silently wrong date.
// countdownSpan normalizes raw through targets.ts's shared date grammar and
// mints the {countdown:<date>}/{countup:<date>} span, or null when either
// step refuses it. Split out of countdownToken so that function's own shape
// stays one branch per outcome instead of stacking both steps into one.
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

// classify resolves one scanned token to its replacement. warned=true marks an
// attempted-but-unmappable variable; the caller reports one warn per distinct
// token.
function classify(token: Token, sink?: FetchSlotSink): TokenResult {
  if (token.head === '') return { repl: token.raw, warned: token.rest !== '' };
  const simple = SIMPLE_TOKENS[token.head];
  if (simple !== undefined) return token.rest === '' ? { repl: simple, warned: false } : literal(token);
  const word = positionalToken(token);
  if (word) return { repl: word, warned: false };
  if (FETCH_HEADS.has(token.head)) return fetchToken(token, sink);
  const special = specialToken(token);
  if (special) return special;
  // eval, weather, userlevel, …
  return literal(token);
}

// specialToken tries every remaining source-specific rule in order: the
// first one that recognizes the token (time, countdown/countup, twitch)
// decides the result. Split out of classify so classify's own shape stays
// one branch per token family instead of one per rule tried within a family.
function specialToken(token: Token): TokenResult | null {
  return timeToken(token) ?? countdownToken(token) ?? twitchToken(token);
}

// fetchToken extracts one urlfetch/customapi call into a synthesized
// definition. Extraction is safe by construction: the URL is copied byte-exact
// out of the response text (no fetch, no resolution, no key handling happens
// here) and the response keeps working at runtime through the reviewed,
// sandboxed definition instead of an unreviewed URL pasted into chat text.
// Without a sink (timers carry no command name to build a slug from) or with
// unusable arguments the token stays literal and warned.
function fetchToken(token: Token, sink?: FetchSlotSink): TokenResult {
  if (!sink) return literal(token);
  const args = parseFetchArgs(token.rest);
  if (!args) return literal(token);
  const key = sink.acquire(args.url);
  if (key === null) return literal(token);
  const span = emit('urlfetch', key);
  if (span === null) return literal(token);
  return { repl: span, warned: false, jsonFetch: args.json };
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
