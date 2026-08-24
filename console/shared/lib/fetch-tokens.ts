// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The {urlfetch:...} token and slug grammar: slugification, dot-path
// building/parsing, and the response scanners the editor and validators share.

import { FETCH_NAME_MAX, PATH_SEGMENT_RE } from './fetch-validate';

/** The bare trigger discipline of normName applied to a def slug: trim,
 * lower-case, fold every non-grammar rune run to "_", trim "_" edges. Empty
 * when the input carries no usable character at all. */
export function slugifyName(s: string): string {
  // Underscore edges are trimmed by index scan, not /^_+/ and /_+$/ — those
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
 * bare digits) — the exact spelling inside `{urlfetch:name.<path>}` that the
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

/**
 * Distinct `{urlfetch:<payload>}` payloads in first-appearance order — the
 * byte-for-byte twin of sesame's urlFetchNames scan (fast-path Contains, then
 * Index('{urlfetch:') / IndexByte('}')). Payloads fold to lower-case because
 * def names are stored bare/lower-case; repeats collapse so one definition
 * referenced three times still costs one fetch, exactly as the engine dedupes.
 */
export function urlFetchNames(response: string): string[] {
  if (!response.includes('{urlfetch')) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const s of fetchSpans(response).spans) {
    const name = (s.payload ?? '').toLowerCase();
    if (name !== '' && !seen.has(name)) {
      seen.add(name);
      out.push(name);
    }
  }
  return out;
}

/**
 * `{urlfetch…` spans that can never resolve — unclosed brace, empty payload,
 * or a payload failing the name/path grammar. The source view flags these
 * verbatim (mark.unknown treatment): typos stay visible, matching the
 * engine's leave-unknown-tokens-literal rule.
 */

interface FetchSpan {
  span: string;
  /** Text after the first ':' inside the braces; null when the token has none. */
  payload: string | null;
}

// fetchSpans walks every '{urlfetch'-prefixed span; dangling carries an
// unclosed trailing token verbatim.
function fetchSpans(response: string): { spans: FetchSpan[]; dangling: string | null } {
  const spans: FetchSpan[] = [];
  let i = response.indexOf('{urlfetch');
  while (i >= 0) {
    const end = response.indexOf('}', i + 1);
    if (end < 0) return { spans, dangling: response.slice(i) };
    const span = response.slice(i, end + 1);
    const body = span.slice(1, -1);
    const colon = body.indexOf(':');
    spans.push({ span, payload: colon < 0 ? null : body.slice(colon + 1) });
    i = response.indexOf('{urlfetch', end);
  }
  return { spans, dangling: null };
}

function malformedFetchSpan(s: FetchSpan): boolean {
  if (s.payload === null || s.payload === '') return true;
  if (s.payload.includes(':')) return true;
  return parseJsonPath(s.payload.toLowerCase()) === null;
}

export function malformedUrlFetchTokens(response: string): string[] {
  const { spans, dangling } = fetchSpans(response);
  const bad = spans.filter(malformedFetchSpan).map((s) => s.span);
  if (dangling !== null) bad.push(dangling);
  return bad;
}

/** Hostname shape-check mirroring the Go denylist for author feedback:
 * IP literals (dotted quad or any ':'-bearing IPv6 form), localhost and the
 * .local/.internal suffixes are rejected at save AND fetch time server-side;
 * this browser-side mirror exists because the shared module runs in the
 * browser too and must not import node's net. The Go gate stays authoritative
 * (it also re-checks DNS-era changes and the IP-logger floor). */
