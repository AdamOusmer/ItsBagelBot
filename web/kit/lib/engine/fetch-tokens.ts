// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The {urlfetch:...} token and slug grammar: slugification, dot-path
// building/parsing, and the response scanners the editor and validators share.

import { FETCH_NAME_MAX, PATH_SEGMENT_RE } from './fetch-validate';
import { lex, type VarToken } from './tmpl';

/** The bare trigger discipline of normName applied to a def slug: trim,
 * lower-case, fold every non-grammar rune run to "_", trim "_" edges. Empty
 * when the input carries no usable character at all. */
export function slugifyName(s: string): string {
  // Underscore edges are trimmed by index scan, not /^_+/ and /_+$/: those
  // anchors backtrack polynomially on adversarial runs of "_" (CodeQL
  // js/polynomial-redos), and this input is broadcaster-typed. The remaining
  // char-class fold is unambiguous and linear.
  const folded = s.trim().toLowerCase().replace(/[^a-z0-9_]+/g, '_');
  return trimUnderscores(trimUnderscores(folded).slice(0, FETCH_NAME_MAX));
}

function trimUnderscores(s: string): string {
  let start = 0;
  let end = s.length;
  while (start < end && s[start] === '_') start++;
  while (end > start && s[end - 1] === '_') end--;
  return s.slice(start, end);
}


/**
 * Dotted token form of a path ('forecast.current.temp_f', array indices as
 * bare digits): the exact spelling inside `{urlfetch:name.<path>}` that the
 * Go dot-path extractor reads.
 */
export function buildJsonPath(segments: string[]): string {
  return segments.join('.');
}

/**
 * Inverse of buildJsonPath, validated against the resolver grammar. Returns
 * null when any segment is malformed so callers can reject instead of storing
 * a path the engine would silently misread.
 */
export function parseJsonPath(dotted: string): string[] | null {
  if (dotted === '') return [];
  const segments = dotted.split('.');
  if (segments.some((s) => !PATH_SEGMENT_RE.test(s))) return null;
  return segments;
}

/** The token name the fetch family answers to, as ./tmpl folds it. */
const URLFETCH = 'urlfetch';

/**
 * Fold one span's payload into the definition key the engine plans against:
 * scope.NormalizeName — trim, strip ONE leading "!", trim, lower-case — over
 * the WHOLE payload, dotted path included, because scope.External keys its
 * fetched values by exactly that string ({urlfetch:w.temp} and
 * {urlfetch:w.hum} are two distinct fetches, `{urlfetch:Temp}` and
 * `{URLFETCH:temp}` are one).
 *
 * '' means the span names no definition ({urlfetch} / {urlfetch:}), which the
 * engine leaves literal.
 */
export function fetchDefKey(payload: string | null): string {
  if (payload === null) return '';
  return payload.trim().replace(/^!/, '').trim().toLowerCase();
}

/**
 * Every `{urlfetch…}` span, in first-appearance order, as the SHARED lexer
 * reads them.
 *
 * Decision record: this used to hand-scan for the literal bytes '{urlfetch',
 * then for the next '}'. That scan was wrong about the grammar it claimed to
 * mirror in two ways the engine has never shared: it was case-SENSITIVE (so
 * `{URLFETCH:weather}` was invisible here while sesame planned it, because
 * tmpl lower-cases every token name), and it read the fallback as part of the
 * payload (so `{urlfetch:weather|n/a}` yielded the bogus definition name
 * "weather|n/a" and was reported to the author as malformed, while the bot
 * fetched "weather" and printed "n/a" on an empty answer). Going through lex()
 * is what makes those two impossible rather than merely fixed.
 */
function fetchSpans(response: string): VarToken[] {
  if (!response.includes('{')) return [];
  return lex(response).filter((t): t is VarToken => t.kind === 'var' && t.name === URLFETCH);
}

/**
 * Distinct definition keys a response references, in first-appearance order.
 * Repeats collapse so one definition referenced three times still costs one
 * fetch, exactly as scope.External's fetchNames dedupes.
 */
export function urlFetchNames(response: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const span of fetchSpans(response)) {
    const name = fetchDefKey(span.payload);
    if (name === '' || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

function malformedFetchSpan(span: VarToken): boolean {
  const key = fetchDefKey(span.payload);
  if (key === '') return true;
  if (key.includes(':')) return true;
  return parseJsonPath(key) === null;
}

/**
 * An unterminated "{urlfetch…" tail, or null.
 *
 * The lexer deliberately opens NO span for a '{' with no '}' after it — the
 * rest of the template is one literal run, which is exactly what chat prints —
 * so this tail cannot come out of lex() and is recognised here instead of
 * pretending the lexer produced a span for it.
 */
function danglingFetchSpan(response: string): string | null {
  const open = response.lastIndexOf('{');
  if (open < 0 || response.includes('}', open)) return null;
  const tail = response.slice(open);
  return tail.slice(1, 1 + URLFETCH.length).toLowerCase() === URLFETCH ? tail : null;
}

/**
 * `{urlfetch…}` spans that can never resolve: unclosed brace, empty payload,
 * or a payload failing the name/path grammar. The source view flags these
 * verbatim (mark.unknown treatment): typos stay visible, matching the
 * engine's leave-unknown-tokens-literal rule.
 */
export function malformedUrlFetchTokens(response: string): string[] {
  const bad = fetchSpans(response).filter(malformedFetchSpan).map((span) => span.raw);
  const dangling = danglingFetchSpan(response);
  if (dangling !== null) bad.push(dangling);
  return bad;
}

/** Hostname shape-check mirroring the Go denylist for author feedback:
 * IP literals (dotted quad or any ':'-bearing IPv6 form), localhost and the
 * .local/.internal suffixes are rejected at save AND fetch time server-side;
 * this browser-side mirror exists because the shared module runs in the
 * browser too and must not import node's net. The Go gate stays authoritative
 * (it also re-checks DNS-era changes and the IP-logger floor). */
