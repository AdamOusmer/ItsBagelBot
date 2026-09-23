// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// twitch.ts translates Nightbot's $(twitch <channel> "<format>") call: the
// field table it substitutes against, the call's own two-argument parse, and
// the re-lex guard that confirms every minted span survives intact inside
// the broadcaster's surrounding format-string text. Split out of
// variables.ts (which keeps every other $(…) rule) so each piece here stays
// a single flat function.

import { emit } from '../targets';
import type { Concept } from '../targets';
import { mappedSpans } from '../validate';
import type { Token } from './scan';
import { literal } from './variables';
import type { TokenResult } from './variables';

// TWITCH_FIELDS maps one {{field}} name (lower-cased) from Nightbot's
// $(twitch <channel> "<format>") call onto the concept it answers, and
// whether that concept accepts the OTHER-channel payload form: title/game/
// uptime do (variables.ts's catalog lists a `{head:<channel>}` form for each),
// channel.viewers/followers/subs do not (one form only), so a field asked of
// another channel that has no such form stays unmapped rather than silently
// answering for the wrong channel.
const TWITCH_FIELDS: Record<string, { concept: Concept; otherChannel: boolean }> = {
  title: { concept: 'title', otherChannel: true },
  game: { concept: 'game', otherChannel: true },
  uptimelength: { concept: 'uptime', otherChannel: true },
  viewers: { concept: 'channel.viewers', otherChannel: false },
  followers: { concept: 'followers', otherChannel: false },
  subscribercount: { concept: 'subs', otherChannel: false }
};

// TWITCH_CALL reads $(twitch <channel> "<format>")'s two arguments: the
// channel (either the already-translated {channel} token from an inner
// $(channel) call, or a bare/quoted login naming another channel) and the
// quoted format string. Nightbot's own call has no other shape.
const TWITCH_CALL = /^\s+("[^"]*"|\S+)\s+"([^"]*)"\s*$/;

// TWITCH_FIELD_REF matches one "{{field}}" placeholder inside the format
// string.
const TWITCH_FIELD_REF = /\{\{(\w+)\}\}/g;

function unquote(s: string): string {
  return s.startsWith('"') && s.endsWith('"') ? s.slice(1, -1) : s;
}

// fieldUnavailable is true when a {{field}} name has no TWITCH_FIELDS entry
// at all, or names one that has no other-channel form and this call asked
// for another channel's value. The && lives in this bare return rather than
// inside an if, which is what keeps the caller's own conditional simple.
function fieldUnavailable(field: string, ownChannel: boolean): boolean {
  const rule = TWITCH_FIELDS[field.toLowerCase()];
  if (!rule) return true;
  return !ownChannel && !rule.otherChannel;
}

// substituteFields replaces every {{field}} this bot can answer inside fmt,
// leaving every other field's raw "{{field}}" text untouched. Returns the
// substituted text, the list of spans it minted (with multiplicity, for the
// re-lex guard below), and whether any field was left unmapped.
function substituteFields(
  fmt: string,
  ownChannel: boolean,
  channelArg: string
): { repl: string; minted: string[]; anyUnmapped: boolean } {
  const minted: string[] = [];
  let anyUnmapped = false;
  const repl = fmt.replace(TWITCH_FIELD_REF, (raw: string, field: string) => {
    if (fieldUnavailable(field, ownChannel)) {
      anyUnmapped = true;
      return raw;
    }
    const rule = TWITCH_FIELDS[field.toLowerCase()];
    const span = ownChannel ? emit(rule.concept) : emit(rule.concept, channelArg);
    if (span === null) {
      anyUnmapped = true;
      return raw;
    }
    minted.push(span);
    return span;
  });
  return { repl, minted, anyUnmapped };
}

// spansSurvive re-lexes text and checks that every span in minted (with
// multiplicity — the same field can appear twice in one format string)
// still comes back as its own intact var token, byte for byte.
//
// Round-trip guard: unlike every other rule in variables.ts, which mints ONE
// span alone, substituteFields above mixes minted spans into the
// broadcaster's own surrounding format-string text. A stray '{'/'}'/'|' in
// THAT text could still merge with a minted span into something tmpl.ts's
// lexer reads as a single different token (a '{' just before a minted
// "{game}" opens a span that does not close until "{game}"'s OWN '}' — see
// tmpl.ts's docs on why intactSpan round-trips every mint) even though each
// span mapped cleanly on its own.
function spansSurvive(text: string, minted: string[]): boolean {
  const found = mappedSpans(text).map((t) => t.raw);
  for (const span of minted) {
    const at = found.indexOf(span);
    if (at === -1) return false;
    found.splice(at, 1);
  }
  return true;
}

// twitchToken maps $(twitch <channel> "<format>") by substituting every
// {{field}} this bot has a fact for and leaving every other field literal —
// the surrounding format-string TEXT is kept either way (a broadcaster's own
// sentence around the fact is not something this importer may drop), which is
// why this returns a compound `repl` (literal text plus zero or more minted
// spans) instead of a single span the way every other rule in variables.ts
// does. `warned` is true when at least one field could not be mapped; the
// specific field name(s) are not distinguished further because variables.ts's
// Warnings keys one warning per distinct RAW token, and this token is one
// $(twitch …) call.
export function twitchToken(token: Token): TokenResult | null {
  if (token.head !== 'twitch') return null;
  const m = TWITCH_CALL.exec(token.rest);
  if (!m) return literal(token);
  const channelArg = unquote(m[1]);
  // Compared against emit('channel') rather than a hardcoded '{channel}'
  // literal, so this recognition can never drift from what $(channel) itself
  // (translated in an earlier pass) actually mints.
  const ownChannel = channelArg === emit('channel');
  const { repl, minted, anyUnmapped } = substituteFields(m[2], ownChannel, channelArg);
  if (anyUnmapped) return { repl, warned: true };
  // Refuse the whole call, rather than ship a token that reads right here
  // and resolves to something else in chat, when the re-lex guard fails.
  if (!spansSurvive(repl, minted)) return literal(token);
  return { repl, warned: false };
}
