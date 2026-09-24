// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { FETCH_NAME_MAX, PATH_SEGMENT_RE } from './fetch-validate';
import { lex, type VarToken } from './tmpl';

export function slugifyName(s: string): string {
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


export function buildJsonPath(segments: string[]): string {
  return segments.join('.');
}

export function parseJsonPath(dotted: string): string[] | null {
  if (dotted === '') return [];
  const segments = dotted.split('.');
  if (segments.some((s) => !PATH_SEGMENT_RE.test(s))) return null;
  return segments;
}

const URLFETCH = 'urlfetch';

export function fetchDefKey(payload: string | null): string {
  if (payload === null) return '';
  return payload.trim().replace(/^!/, '').trim().toLowerCase();
}

function fetchSpans(response: string): VarToken[] {
  if (!response.includes('{')) return [];
  return lex(response).filter((t): t is VarToken => t.kind === 'var' && t.name === URLFETCH);
}

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

function danglingFetchSpan(response: string): string | null {
  const open = response.lastIndexOf('{');
  if (open < 0 || response.includes('}', open)) return null;
  const tail = response.slice(open);
  return tail.slice(1, 1 + URLFETCH.length).toLowerCase() === URLFETCH ? tail : null;
}

export function malformedUrlFetchTokens(response: string): string[] {
  const bad = fetchSpans(response).filter(malformedFetchSpan).map((span) => span.raw);
  const dangling = danglingFetchSpan(response);
  if (dangling !== null) bad.push(dangling);
  return bad;
}
