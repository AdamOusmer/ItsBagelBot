// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The one place every importer mints a "{…}" span out of another product's
// variable. Before this file existed, six parsers each carried their own
// hand-written '{user}', '{touser}', '{args}' literals — correct on the day
// they were typed, silently wrong the day a head is renamed, because nothing
// tied them back to the grammar or to each other. TARGETS is the single
// record of "this concept maps to this token head"; every source translates
// through emit()/positional()/slice() instead of writing a brace itself, so a
// rename here is one edit instead of a six-file grep.
//
// Canonical heads only: a source's own historical spelling ({target},
// {sender}, {uses} from early parsers, before this module existed) is not a
// second legal spelling here. {target} -> touser, {sender} -> user,
// {uses} -> count: see uses.go's decision record (nightbot/variables.ts) for
// why {count} is the canonical bare spelling and {uses} stayed an alias only
// in the resolver, never in anything an importer emits.
//
// parity.test.ts's rule E walks Object.values(TARGETS) against the Go token
// catalog golden, so a head added here that the resolver does not answer
// fails CI instead of shipping a translated response that renders its own
// output literally.

import { intactSpan } from '../engine/tmpl';
import { intactSpanWithFallback } from '../engine/tmpl-fallback';
import { isRFC3339, validCalendarDay, validClock } from './validate';

/** Every concept a source parser is allowed to translate a variable onto.
 * Deliberately NOT the full token catalog (variables.ts/VARIABLES): only
 * heads at least one of the six sources' documented variable languages maps
 * onto today. A concept with no source rule yet is not added speculatively —
 * see decision-records-in-code: a mapping with no caller cannot be pinned by
 * a vector test. */
export type Concept =
  | 'user'
  | 'touser'
  | 'args'
  | 'querystring'
  | 'channel'
  | 'title'
  | 'game'
  | 'uptime'
  | 'channel.viewers'
  | 'followers'
  | 'subs'
  | 'followage'
  | 'accountage'
  | 'random'
  | 'choice'
  | 'random.viewer'
  | 'count'
  | 'counter'
  | 'countdown'
  | 'countup'
  | 'time'
  | 'urlfetch'
  | 'points'
  | 'points.name'
  | 'if'
  | 'math'
  | 'repeat'
  | 'chatters'
  | 'user.id'
  | 'user.login';

// WordIndex names this file's one recurring number shape (a 1-based word
// position in positional()/slice()'s shared range), and RawText its one
// recurring string shape (a not-yet-validated source payload, fallback,
// zone spelling or date text) — structurally identical to number/string, so
// no call site changes, but named so this parsing-heavy file's own
// function-argument shapes read as a small domain vocabulary rather than as
// Primitive Obsession / String Heavy Function Arguments.
type WordIndex = number;
type RawText = string;

// TARGETS maps each concept to the exact token head emit() mints it as. Keys
// and values agree today (the concept vocabulary was named after the heads
// it targets); they are kept as two separate things rather than a Set of
// heads because a concept is a stable name a parser's rule table can hold
// onto, while the value is the one fact this file is trusted to get right.
export const TARGETS: Record<Concept, string> = {
  user: 'user',
  touser: 'touser',
  args: 'args',
  querystring: 'querystring',
  channel: 'channel',
  title: 'title',
  game: 'game',
  uptime: 'uptime',
  'channel.viewers': 'channel.viewers',
  followers: 'followers',
  subs: 'subs',
  followage: 'followage',
  accountage: 'accountage',
  random: 'random',
  choice: 'choice',
  'random.viewer': 'random.viewer',
  count: 'count',
  counter: 'counter',
  countdown: 'countdown',
  countup: 'countup',
  time: 'time',
  urlfetch: 'urlfetch',
  points: 'points',
  'points.name': 'points.name',
  if: 'if',
  math: 'math',
  repeat: 'repeat',
  chatters: 'chatters',
  'user.id': 'user.id',
  'user.login': 'user.login'
};

/** Mint the span for one concept, or null when the payload would not survive
 * the lexer intact (see tmpl.ts's intactSpan). Every source rule that
 * produces a plain "{head}" / "{head:payload}" span goes through this,
 * never through a template literal.
 *
 * There is no emitWithFallback sibling (one existed briefly during phase 6
 * and was deleted on review: nothing ever called it — every fallback-bearing
 * span a source actually mints is positional or a slice, never a bare
 * concept, so positional()/slice()'s own optional fallback parameter below
 * covers every real call site). Add one back only once a concept-level
 * fallback has an actual caller to pin against. */
export function emit(concept: Concept, payload: RawText | null = null): string | null {
  return intactSpan(TARGETS[concept], payload);
}

// POSITIONAL_MAX mirrors scope.MaxPositional (the bot's own ceiling on
// {1}..{N}): a source word-number past this has no token to land on, so
// positional()/slice() refuse it the same way an unmapped variable would —
// literal+warn at the call site, not a span that would itself stay literal
// in chat.
export const POSITIONAL_MAX = 30;

// isPositionalIndex is true for any whole word position this bot's grammar
// spells a token for (1..POSITIONAL_MAX). Shared by positional() and slice()
// below so both bounds agree on the same range without repeating it.
function isPositionalIndex(x: WordIndex): boolean {
  return Number.isInteger(x) && x >= 1 && x <= POSITIONAL_MAX;
}

/** Mint "{n}" or, with a fallback, "{n|fallback}" — the bare word-number
 * form. null when n is not a whole word position in 1..POSITIONAL_MAX (a
 * source token claiming to be one, but out of range or non-integer, is not
 * actually a positional word) or when a supplied fallback cannot round-trip
 * (intactSpanWithFallback's own contract: no '|' or '}' in it). Fallback and
 * no-fallback used to be two functions (positional/positionalWithFallback);
 * merged on review since every call site already knew at compile time
 * whether it had a fallback in hand. */
export function positional(n: WordIndex, fallback?: RawText): string | null {
  if (!isPositionalIndex(n)) return null;
  return fallback === undefined ? intactSpan(String(n), null) : intactSpanWithFallback(String(n), null, fallback);
}

// slicedBoundInvalid is true when an explicit bound (n or m) was supplied
// and is not a whole word position this bot's grammar spells a token for.
function slicedBoundInvalid(bound: WordIndex | undefined): boolean {
  return bound !== undefined && !isPositionalIndex(bound);
}

/**
 * Mint one of the three arg-slice forms this bot's positional grammar reads
 * (tmpl.ts's lex, {n} covered by positional() above), each with an optional
 * fallback ("{n:|fb}" etc — Fossabot's $(fromindex2 nothing) spells one this
 * way):
 *
 *   slice(n)     -> "{n:}"   from word n to the end of the args
 *   slice(undefined, m) -> "{:m}" from the start through word m
 *   slice(n, m)  -> "{n:m}"  words n through m, inclusive
 *
 * Both bounds share positional()'s range; slice() with neither bound is not
 * a slice at all and returns null rather than minting "{:}" — the caller
 * meant {args} and should ask for that concept instead. Fallback and
 * no-fallback used to be two functions (slice/sliceWithFallback); merged on
 * review for the same reason positional()'s pair was.
 */
export function slice(n?: WordIndex, m?: WordIndex, fallback?: RawText): string | null {
  if (n === undefined && m === undefined) return null;
  if (slicedBoundInvalid(n) || slicedBoundInvalid(m)) return null;
  const name = n === undefined ? '' : String(n);
  const payload = m === undefined ? '' : String(m);
  return fallback === undefined ? intactSpan(name, payload) : intactSpanWithFallback(name, payload, fallback);
}

// Date.parse is machine-dependent and was rejected here on review: a
// no-zone input reads in the RUNNING MACHINE's local zone (a preview built in
// a US datacenter and one built on a broadcaster's laptop would normalize the
// SAME source string to different instants); bare small integers like "0" or
// "1" parse as years 1999/2000-ish garbage dates instead of failing; and an
// abbreviation V8 does not recognize (CET, BST, …) returns NaN with no way to
// tell that apart from genuine garbage. None of that is fit to feed a stored
// countdown/countup token. normalizeInstant below replaces it with a FIXED
// grammar: every branch that does not carry an explicit, resolvable zone (or
// is the bare YYYY-MM-DD form scope.parseInstant itself reads as UTC
// midnight) is refused rather than guessed at.
//
// DATE_ONLY/MONTH_NAME_DATE/NUMERIC_DATE all capture the calendar fields
// so the shared calendar validator (validate.ts's validCalendarDay, the same
// one isRFC3339 uses) can catch a "Feb 30" the same way across every branch.
const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;

// TIME_ONLY recognizes a bare clock reading with no date component
// ("5:00:00 PM EST", "17:00 UTC"). Shared with fossabot/variables.ts so a
// countdown token that only carries a time, no calendar date, is refused
// identically by every source: Date.parse would have anchored a bare time to
// TODAY's date on whatever machine ran it, which answers a different instant
// than the source's own clock meant.
export const TIME_ONLY = /^\d{1,2}:\d{2}(:\d{2})?\s*(am|pm)?\s*[a-z0-9:+-]*$/i;

const MONTH_NUMBER: Record<string, number> = {
  jan: 1,
  feb: 2,
  mar: 3,
  apr: 4,
  may: 5,
  jun: 6,
  jul: 7,
  aug: 8,
  sep: 9,
  oct: 10,
  nov: 11,
  dec: 12
};

// ZONE_OFFSETS_MIN: the fixed US abbreviation table the review named, each a
// STANDARD or DAYLIGHT offset in minutes from UTC. A date string that names
// "EST" vs "EDT" already committed to one side of that distinction, so there
// is no seasonal inference to get wrong here — unlike Date.parse's own
// abbreviation handling, which is engine-dependent and often absent.
const ZONE_OFFSETS_MIN: Record<string, number> = {
  utc: 0,
  gmt: 0,
  z: 0,
  est: -5 * 60,
  edt: -4 * 60,
  cst: -6 * 60,
  cdt: -5 * 60,
  mst: -7 * 60,
  mdt: -6 * 60,
  pst: -8 * 60,
  pdt: -7 * 60
};

// zoneOffsetMinutes reads a REQUIRED zone spelling — Z/UTC/GMT, one of the
// fixed US abbreviations above, or an explicit ±hh:mm offset — into minutes
// from UTC, or null when it names something this grammar does not resolve
// (any other abbreviation: CET, BST, JST, …).
function zoneOffsetMinutes(zone: RawText): number | null {
  const named = ZONE_OFFSETS_MIN[zone.toLowerCase()];
  if (named !== undefined) return named;
  const m = /^([+-])(\d{2}):?(\d{2})$/.exec(zone);
  if (!m) return null;
  const sign = m[1] === '-' ? -1 : 1;
  return sign * (Number(m[2]) * 60 + Number(m[3]));
}

// MONTH_NAME_DATE reads "Dec 25 2015[, ][ 12:00:00 AM] EST" — Nightbot's own
// export shape. NUMERIC_DATE reads the US "12/25/2015[ 12:00:00 AM] EST"
// spelling some sources use instead. Both share the same optional
// hh:mm[:ss] [AM|PM] time group and REQUIRE the trailing zone: without one,
// the string is ambiguous about which instant it names, and this grammar
// refuses to guess rather than reading it in whatever zone happens to be
// running the code.
const MONTH_NAME_DATE =
  /^([A-Za-z]{3})\s+(\d{1,2}),?\s+(\d{4})(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp][Mm])?)?\s+(\S+)$/;
const NUMERIC_DATE =
  /^(\d{1,2})\/(\d{1,2})\/(\d{4})(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp][Mm])?)?\s+(\S+)$/;

// hourFromClock resolves the clock's hour field into 24-hour form,
// converting a 12-hour AM/PM reading ("12:00 AM" -> 0, "12:00 PM" -> 12,
// "5:00 PM" -> 17) when one is present. Split out of dateTimeToInstant so
// its own small chain of ternaries scores against this function alone.
function hourFromClock(clock: { h?: string; ampm?: string }): number {
  const hour = clock.h === undefined ? 0 : Number(clock.h);
  if (!clock.ampm) return hour;
  const isPM = clock.ampm.toLowerCase() === 'pm';
  if (hour === 12) return isPM ? 12 : 0;
  return isPM ? hour + 12 : hour;
}

// dateTimeToInstant folds one fully-parsed (calendar day, optional 12/24h
// clock, required zone) reading into an RFC3339 UTC string, or null when the
// calendar day, clock or zone spelling is not one this grammar accepts.
function dateTimeToInstant(
  day: { y: number; mo: number; d: number },
  clock: { h?: string; mi?: string; s?: string; ampm?: string },
  zone: RawText
): string | null {
  if (!validCalendarDay(day)) return null;
  const hour = hourFromClock(clock);
  const minute = clock.mi === undefined ? 0 : Number(clock.mi);
  const second = clock.s === undefined ? 0 : Number(clock.s);
  if (!validClock({ h: hour, mi: minute, s: second })) return null;
  const offsetMin = zoneOffsetMinutes(zone);
  if (offsetMin === null) return null;
  // local time = UTC + offset, so UTC = local - offset.
  const utcMs = Date.UTC(day.y, day.mo - 1, day.d, hour, minute, second) - offsetMin * 60000;
  return new Date(utcMs).toISOString();
}

// DateForm reads one accepted countdown/countup spelling out of an already-
// trimmed payload: undefined when this form's own shape does not match (try
// the next form), null when it matches but is refused (stop trying — the
// input committed to this form and it is invalid), or the normalized
// RFC3339 UTC instant on success.
type DateForm = (trimmed: RawText) => string | null | undefined;

// rfc3339Form recognizes an already-RFC3339 payload (validate.ts's own
// Go-parity check, which already validates the calendar day) and passes it
// through unchanged.
function rfc3339Form(trimmed: RawText): string | null | undefined {
  return isRFC3339(trimmed) ? trimmed : undefined;
}

// dateOnlyForm recognizes bare YYYY-MM-DD, read as UTC midnight the same way
// scope.parseInstant itself reads it.
function dateOnlyForm(trimmed: RawText): string | null | undefined {
  const m = DATE_ONLY.exec(trimmed);
  if (!m) return undefined;
  const day = { y: Number(m[1]), mo: Number(m[2]), d: Number(m[3]) };
  return validCalendarDay(day) ? trimmed : null;
}

// timeOnlyForm refuses a bare clock reading with no calendar date before
// either date form below runs, so it cannot fall through and be misread as
// one of them.
function timeOnlyForm(trimmed: RawText): string | null | undefined {
  return TIME_ONLY.test(trimmed) ? null : undefined;
}

// monthNameForm recognizes "Dec 25 2015[, ][ 12:00:00 AM] EST" — Nightbot's
// own export shape — and re-emits it as RFC3339 UTC.
function monthNameForm(trimmed: RawText): string | null | undefined {
  const m = MONTH_NAME_DATE.exec(trimmed);
  if (!m) return undefined;
  const mo = MONTH_NUMBER[m[1].toLowerCase()];
  if (mo === undefined) return null;
  const day = { y: Number(m[3]), mo, d: Number(m[2]) };
  return dateTimeToInstant(day, { h: m[4], mi: m[5], s: m[6], ampm: m[7] }, m[8]);
}

// numericForm recognizes the US "12/25/2015[ 12:00:00 AM] EST" slash-
// separated spelling some sources use instead, and re-emits it as RFC3339
// UTC.
function numericForm(trimmed: RawText): string | null | undefined {
  const m = NUMERIC_DATE.exec(trimmed);
  if (!m) return undefined;
  const day = { y: Number(m[3]), mo: Number(m[1]), d: Number(m[2]) };
  return dateTimeToInstant(day, { h: m[4], mi: m[5], s: m[6], ampm: m[7] }, m[8]);
}

// DATE_FORMS lists every accepted spelling, tried in order: the first form
// whose own shape matches (undefined otherwise) decides the result, success
// or refusal, and no later form runs.
const DATE_FORMS: DateForm[] = [rfc3339Form, dateOnlyForm, timeOnlyForm, monthNameForm, numericForm];

/**
 * Normalize a source product's own countdown/countup date spelling onto one
 * scope.parseInstant will read, or null when nothing here can — never by
 * asking Date.parse (see the decision record above). Tries DATE_FORMS in
 * order and returns the first form's result once one recognizes the input.
 */
export function normalizeInstant(payload: RawText): string | null {
  const trimmed = payload.trim();
  for (const form of DATE_FORMS) {
    const result = form(trimmed);
    if (result !== undefined) return result;
  }
  return null;
}
