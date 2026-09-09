// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The console-side mirror of pkg/tmpl: the one {token} lexer every chat
// surface shares, ported so the dashboard rehearsal and the marketing site's
// command builder read a broadcaster's template exactly the way the bot does.
//
// The two implementations are pinned against ONE fixture — pkg/tmpl's
// testdata/tokens.golden.json, asserted here by tmpl.test.ts — so a grammar
// change that lands in only one language fails both suites rather than
// shipping a preview that lies about what chat will see.
//
// Grammar (identical to pkg/tmpl, read that file for the reasoning):
//   - a span opens at '{' and closes at the FIRST '}'; a '{' with no closing
//     brace opens nothing and the rest of the template is one literal
//   - name = the text before the first ':', lower-cased (token names are
//     case-insensitive)
//   - payload = everything after that ':', case preserved, because a payload
//     is data ({choice:Hi,Yo} must be able to offer "Hi")
//   - fallback = the text after the span's LAST '|', rendered when the name
//     resolves to an empty value
//
// There is no nesting in v1, but this is a real tokenizer rather than a
// find-and-replace so one level can be added later without any caller
// relearning the shape.

/** A run of text copied verbatim. */
export interface LiteralToken {
  kind: 'literal';
  text: string;
}

/** One "{…}" span, split into the parts a resolver plans against. */
export interface VarToken {
  kind: 'var';
  /** Lower-cased name: {User} and {USER} both lex to "user". */
  name: string;
  /** Text after the first ':', case preserved. null when the span carries no
   * ':' at all — the engine draws a real distinction there: {choice} is an
   * unknown name and stays literal, {choice:} is an (empty) option list that
   * resolves. */
  payload: string | null;
  /** Text after the span's last '|'. null when the span declared none, which
   * is distinct from an explicit empty one ({x|}). */
  fallback: string | null;
  /** The span exactly as written, braces included: what an unknown name
   * renders as, so a typo stays visible to its author. */
  raw: string;
  /** "name", or "name:payload": the string a resolver is asked for. It is the
   * pre-fallback key format on purpose, so adding '|' to the grammar did not
   * force a single existing resolver to be rewritten. */
  key: string;
}

export type Token = LiteralToken | VarToken;

/** Split a template into literal runs and "{…}" spans. Concatenating every
 * token's `text`/`raw` reproduces the input exactly. */
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
    if (end < 0) break; // no closing brace: the rest is one literal run
    pushLiteral(out, template.slice(from, i));
    out.push(parseSpan(template.slice(i, end + 1)));
    i = end + 1;
    from = i;
  }
  pushLiteral(out, template.slice(from));
  return out;
}

/** Adds a literal run, skipping the empty one two adjacent spans produce, so
 * a token list never carries filler. */
function pushLiteral(out: Token[], text: string): void {
  if (text !== '') out.push({ kind: 'literal', text });
}

/**
 * Split one "{…}" span (braces included) into its parts.
 *
 * The fallback is cut FIRST, at the span's LAST '|', so a payload may still
 * carry pipes: {choice:a|b|c} offers "a|b" or falls back to "c" rather than
 * the reverse. Last-wins was chosen over first-wins because the fallback is
 * the trailing, optional part of the grammar an author reads left to right
 * ("this token, or else that text"); first-wins would make every pipe inside
 * a payload silently amputate it. {choice} itself is unaffected either way:
 * its option separator is ',', never '|'.
 */
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

/**
 * Pick the text one span renders for a resolver's answer — the single
 * definition of the three-way rule every surface repeats (pkg/tmpl
 * Token.Resolve):
 *
 *   unknown name (null) -> the raw span, braces included, so a typo stays
 *                          visible to the broadcaster who wrote it
 *   empty value         -> the fallback ("" when the span declared none)
 *   otherwise           -> the value
 *
 * A fallback deliberately does NOT rescue an unknown name: {typo|hi} would
 * otherwise render "hi" and hide the typo forever.
 */
export function resolveToken(token: VarToken, value: string | null): string {
  if (value === null) return token.raw;
  if (value === '') return token.fallback ?? '';
  return value;
}

/** Expand a template with a resolver, returning null from the resolver for a
 * name it does not know. The string-only sibling of pkg/tmpl's Expand; the
 * rehearsal needs per-token segments instead and walks lex() itself. */
export function expand(template: string, resolve: (token: VarToken) => string | null): string {
  return lex(template)
    .map((token) => (token.kind === 'literal' ? token.text : resolveToken(token, resolve(token))))
    .join('');
}
