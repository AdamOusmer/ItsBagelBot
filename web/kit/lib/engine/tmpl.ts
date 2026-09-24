// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface LiteralToken {
  kind: 'literal';
  text: string;
}

export interface VarToken {
  kind: 'var';
  name: string;
  payload: string | null;
  fallback: string | null;
  raw: string;
  key: string;
}

export type Token = LiteralToken | VarToken;

export function lex(template: string): Token[] {
  const out: Token[] = [];
  let from = 0;
  let i = 0;
  while (i < template.length) {
    if (template[i] !== '{') {
      i++;
      continue;
    }
    const end = template.indexOf('}', i + 1);
    if (end < 0) break;
    pushLiteral(out, template.slice(from, i));
    out.push(parseSpan(template.slice(i, end + 1)));
    i = end + 1;
    from = i;
  }
  pushLiteral(out, template.slice(from));
  return out;
}

function pushLiteral(out: Token[], text: string): void {
  if (text !== '') out.push({ kind: 'literal', text });
}

function parseSpan(raw: string): VarToken {
  let body = raw.slice(1, -1);
  let fallback: string | null = null;
  const pipe = body.lastIndexOf('|');
  if (pipe >= 0) {
    fallback = body.slice(pipe + 1);
    body = body.slice(0, pipe);
  }
  const colon = body.indexOf(':');
  if (colon < 0) {
    const name = body.toLowerCase();
    return { kind: 'var', name, payload: null, fallback, raw, key: name };
  }
  const name = body.slice(0, colon).toLowerCase();
  const payload = body.slice(colon + 1);
  return { kind: 'var', name, payload, fallback, raw, key: `${name}:${payload}` };
}

export function resolveToken(token: VarToken, value: string | null): string {
  if (value === null) return token.raw;
  if (value === '') return token.fallback ?? '';
  return value;
}

export function intactSpan(name: string, payload: string | null): string | null {
  const span = payload === null ? `{${name}}` : `{${name}:${payload}}`;
  const tokens = lex(span);
  if (tokens.length !== 1) return null;
  const token = tokens[0];
  if (token.kind !== 'var') return null;
  if (token.name !== name || token.payload !== payload) return null;
  return token.fallback === null ? span : null;
}

export function expand(template: string, resolve: (token: VarToken) => string | null): string {
  return lex(template)
    .map((token) => (token.kind === 'literal' ? token.text : renderSpan(token, resolve)))
    .join('');
}

function renderSpan(token: VarToken, resolve: (token: VarToken) => string | null): string {
  const cond = parseCond(token);
  if (cond === null) return resolveToken(token, resolve(token));
  return condText(token, cond, resolve(cond.ref));
}

const COND_NAME = 'if';

export interface Cond {
  ref: VarToken;
  want: string | null;
  then: string;
  els: string;
}

export function parseCond(token: VarToken): Cond | null {
  if (token.name !== COND_NAME || token.payload === null) return null;
  const parts = token.payload.split(':');
  const last = parts.length - 1;
  if (last < 1) return null;
  const key = last === 1 ? parts[0] : parts.slice(0, last - 1).join(':');
  const then = last === 1 ? parts[1] : parts[last - 1];
  const els = last === 1 ? '' : parts[last];
  const eq = key.indexOf('=');
  if (eq < 0) return { ref: refToken(key), want: null, then, els };
  return { ref: refToken(key.slice(0, eq)), want: key.slice(eq + 1), then, els };
}

function refToken(key: string): VarToken {
  const colon = key.indexOf(':');
  if (colon < 0) {
    const name = key.toLowerCase();
    return { kind: 'var', name, payload: null, fallback: null, raw: '', key: name };
  }
  const name = key.slice(0, colon).toLowerCase();
  const payload = key.slice(colon + 1);
  return { kind: 'var', name, payload, fallback: null, raw: '', key: `${name}:${payload}` };
}

export function condHolds(cond: Cond, value: string): boolean {
  return cond.want === null ? value !== '' : value === cond.want;
}

export function condText(token: VarToken, cond: Cond, value: string | null): string {
  if (value === null) return token.raw;
  return condHolds(cond, value) ? cond.then : cond.els;
}
