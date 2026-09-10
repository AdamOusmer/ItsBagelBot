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
//   - a conditional ({if:cond:then:else}) reads the token its cond NAMES and
//     renders one of two literal branches; see parseCond below and
//     pkg/tmpl/cond.go for where the three fields split
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

/**
 * Build one "{name}" / "{name:payload}" span, or null when it would not
 * survive this lexer intact.
 *
 * Every surface that MINTS a span out of a string it did not write — an
 * importer translating another product's variable, a palette built from a
 * catalog of token names — goes through here. Those strings can carry the two
 * bytes the grammar spends: a '}' closes the span at the first one (amputating
 * the rest) and a '|' re-reads the tail as a fallback. Either byte yields a
 * token that reads correctly wherever it is displayed and resolves to
 * something else in chat, which is the one class of bug the person who caused
 * it cannot see.
 *
 * The check is a round trip through lex() rather than a character denylist, so
 * it cannot fall out of step with the grammar it protects: exactly one var
 * token, the name asked for (already lower-case — a name this lexer would fold
 * is refused, because emitting it would make the round trip a lie), the
 * payload byte for byte, and no fallback nobody asked for.
 */
export function intactSpan(name: string, payload: string | null): string | null {
  const span = payload === null ? `{${name}}` : `{${name}:${payload}}`;
  // The round trip is written here rather than in a helper beside it: the
  // check and the span it guards have to move together, and a helper taking
  // the same name and payload a second time is one more place they can drift.
  const tokens = lex(span);
  if (tokens.length !== 1) return null;
  const token = tokens[0];
  if (token.kind !== 'var') return null;
  if (token.name !== name || token.payload !== payload) return null;
  return token.fallback === null ? span : null;
}

/** Expand a template with a resolver, returning null from the resolver for a
 * name it does not know. The string-only sibling of pkg/tmpl's Expand; the
 * rehearsal needs per-token segments instead and walks lex() itself. */
export function expand(template: string, resolve: (token: VarToken) => string | null): string {
  return lex(template)
    .map((token) => (token.kind === 'literal' ? token.text : renderSpan(token, resolve)))
    .join('');
}

/** One span's worth of rendering: a conditional reads the token its cond
 * names, anything else resolves its own key (pkg/tmpl appendSpan). */
function renderSpan(token: VarToken, resolve: (token: VarToken) => string | null): string {
  const cond = parseCond(token);
  if (cond === null) return resolveToken(token, resolve(token));
  return condText(token, cond, resolve(cond.ref));
}

// --- conditionals (pkg/tmpl/cond.go) --------------------------------------

/** The span name that opens a conditional. */
const COND_NAME = 'if';

/** One "{if:cond:then:else}" span, split into the parts a renderer needs.
 * The mirror of pkg/tmpl's Cond — read cond.go for the reasoning. */
export interface Cond {
  /** The token the test READS: a synthetic span for the cond's key, so
   * "count:deaths" reaches a resolver exactly like a written
   * {count:deaths} would. Its `raw` is never rendered — an unresolvable cond
   * renders the whole {if:…} span literally instead. */
  ref: VarToken;
  /** The literal the value is compared against ("name=lit" form), or null for
   * the bare non-empty test. Comparison is case-SENSITIVE, byte for byte. */
  want: string | null;
  /** Rendered when the test holds / does not. `els` is '' for the two-part
   * form, which is what makes a false test with no else render nothing. */
  then: string;
  els: string;
}

/**
 * Split one span into a conditional, or null when it is not one.
 *
 * The split is anchored on the RIGHT: then and else are the LAST two ':'
 * segments and everything before them is the cond key, so a cond may carry
 * its own payload ({if:count:deaths:none:some}) at the cost of a ':' inside
 * then/else being read as part of the cond. pkg/tmpl's cutCond carries the
 * decision record for why the ambiguity is spent that way round.
 *
 * A payload with no ':' in it is not a conditional: {if}, {if:} and {if:x}
 * fall through to the ordinary path and stay literal, so a half-written
 * conditional is visible to its author rather than quietly rendering nothing.
 */
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

/** Build the synthetic span a cond key names, with the same name/payload
 * split a written {…} produces. `raw` stays empty: a ref is never rendered. */
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

/** Whether a cond holds for its referenced token's value. The bare form tests
 * NON-EMPTINESS rather than truthiness: every value here is text, and "0" is
 * a perfectly good thing for a token to say. */
export function condHolds(cond: Cond, value: string): boolean {
  return cond.want === null ? value !== '' : value === cond.want;
}

/**
 * Render one conditional span from its ref's looked-up value.
 *
 * A null value — nothing in reach resolves the referenced name — renders the
 * whole span literally, braces and all, exactly like any other unknown token:
 * a cond on a name the bot cannot answer is a typo or a module that is off,
 * and silently taking the else branch would hide both.
 */
export function condText(token: VarToken, cond: Cond, value: string | null): string {
  if (value === null) return token.raw;
  return condHolds(cond, value) ? cond.then : cond.els;
}
