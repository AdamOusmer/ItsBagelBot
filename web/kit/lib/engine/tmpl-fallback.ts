// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// intactSpan's sibling for a span that DOES carry a fallback. Split into its
// own module (rather than living beside intactSpan in tmpl.ts) so tmpl.ts
// itself stays the one-lexer file pkg/tmpl's fixture pins against, with
// nothing added past what that fixture covers.

import { lex } from './tmpl';
import type { VarToken } from './tmpl';

// fallbackRoundTrips is false when fallback could never come back out of
// lex() unchanged: parseSpan cuts at the LAST '|', so a second one would be
// re-read as part of the fallback text rather than staying byte-identical,
// and a '}' would close the span early.
function fallbackRoundTrips(fallback: string): boolean {
  return !fallback.includes('|') && !fallback.includes('}');
}

// spanBody joins name and payload the way a "{body|fallback}" span's body
// reads: bare name with no payload, "name:payload" with one.
function spanBody(name: string, payload: string | null): string {
  return payload === null ? name : `${name}:${payload}`;
}

// sameNameAndPayload is true when the lexed var token names and pays exactly
// what the caller asked to mint — the other half of the round-trip check,
// alongside the fallback comparison the caller makes itself.
function sameNameAndPayload(token: VarToken, name: string, payload: string | null): boolean {
  return token.name === name && token.payload === payload;
}

/**
 * intactSpan's sibling for a span that DOES carry a fallback ({name|fb} /
 * {name:payload|fb}) — several source products (StreamLabs Chatbot's
 * $(1|default), SE's $(1|d)) spell a positional-with-default in one token,
 * and importer/targets.ts's emitWithFallback needs the same round-trip
 * guarantee intactSpan gives every other minted span: the fallback the
 * caller supplies is exactly the fallback that comes back out of lex(), not
 * one lex() would read differently because it hid a '}' or a second '|'.
 *
 * A fallback containing '|' cannot ever round-trip: parseSpan cuts at the
 * LAST '|', so a second one would be re-read as part of the fallback text
 * rather than staying byte-identical, and one containing '}' would close the
 * span early. Both are refused before the round trip runs, same as
 * intactSpan refuses a name/payload that would not come back unchanged.
 */
export function intactSpanWithFallback(name: string, payload: string | null, fallback: string): string | null {
  if (!fallbackRoundTrips(fallback)) return null;
  const span = `{${spanBody(name, payload)}|${fallback}}`;
  const tokens = lex(span);
  if (tokens.length !== 1) return null;
  const token = tokens[0];
  if (token.kind !== 'var') return null;
  if (!sameNameAndPayload(token, name, payload)) return null;
  return token.fallback === fallback ? span : null;
}
