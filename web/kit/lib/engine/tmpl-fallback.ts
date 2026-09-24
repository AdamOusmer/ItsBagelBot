// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex } from './tmpl';
import type { VarToken } from './tmpl';

function fallbackRoundTrips(fallback: string): boolean {
  return !fallback.includes('|') && !fallback.includes('}');
}

function spanBody(name: string, payload: string | null): string {
  return payload === null ? name : `${name}:${payload}`;
}

function sameNameAndPayload(token: VarToken, name: string, payload: string | null): boolean {
  return token.name === name && token.payload === payload;
}

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
