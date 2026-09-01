// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The $(…) scanner behind the Nightbot variable layer. Nightbot's only
// delimiter is $(…) — it has no ${…} and no bare-brace shorthand — so this
// scanner never touches plain braces, and a response's punctuation survives it
// untouched.

// Token is one scanned variable reference, already split into the parts the
// token table matches on: `raw` is the source text to keep when nothing maps,
// `head` its lower-cased name, `rest` whatever followed the name (arguments,
// dot-subfields).
export interface Token {
  raw: string;
  head: string;
  rest: string;
  start: number;
  end: number;
}

const NAME_RUN = /^[A-Za-z0-9_]*/;

// nextToken returns the next translatable token at or after `from`, or null.
// A token whose body opens another token is skipped in favour of its interior:
// the composite has no mapping of its own (it is warned as-is on the next pass,
// once its interior reads translated) while the inner leaf lands cleanly now.
export function nextToken(text: string, from: number): Token | null {
  for (let i = text.indexOf('$(', from); i !== -1; i = text.indexOf('$(', i + 1)) {
    const end = matchParen(text, i);
    if (end === -1) continue;
    const body = text.slice(i + 2, end - 1);
    if (body.includes('$(')) continue;
    return { ...split(body.trim()), raw: text.slice(i, end), start: i, end };
  }
  return null;
}

// split separates a token body into its leading name run and the remainder.
function split(body: string): { head: string; rest: string } {
  const run = NAME_RUN.exec(body)?.[0] ?? '';
  return { head: run.toLowerCase(), rest: body.slice(run.length) };
}

// matchParen finds the ) closing the $( opened at start (nesting counted); -1
// when unbalanced, which leaves the token literal.
function matchParen(text: string, start: number): number {
  let depth = 0;
  for (let j = start + 1; j < text.length; j++) {
    if (text[j] === '(') depth++;
    else if (text[j] === ')' && --depth === 0) return j + 1;
  }
  return -1;
}
