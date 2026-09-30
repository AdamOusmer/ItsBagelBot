// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface Token {
  raw: string;
  head: string;
  rest: string;
  start: number;
  end: number;
}

const NAME_RUN = /^[A-Za-z0-9_]*/;

// scanTokens visits the response once and yields translatable leaves in source
// order. Counting every opening parenthesis preserves the old nesting rules;
// remembering the most recent $( opener identifies composite tokens without
// rescanning their bodies. Unmatched groups stay literal while balanced leaves
// inside them can still translate.
// A token whose body opens another token is skipped in favour of its interior:
// the composite has no mapping of its own (it is warned as-is on the next pass,
// once its interior reads translated) while the inner leaf lands cleanly now.
export function* scanTokens(text: string): Generator<Token> {
  const opens: number[] = [];
  let lastTokenStart = -1;
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '(') {
      opens.push(i);
      if (i > 0 && text[i - 1] === '$') lastTokenStart = i - 1;
    } else if (text[i] === ')') {
      const open = opens.pop();
      if (open === undefined || open === 0 || text[open - 1] !== '$') continue;
      const start = open - 1;
      if (lastTokenStart > start) continue;
      const end = i + 1;
      yield { ...split(text.slice(open + 1, i).trim()), raw: text.slice(start, end), start, end };
    }
  }
}

function split(body: string): { head: string; rest: string } {
  const run = NAME_RUN.exec(body)?.[0] ?? '';
  return { head: run.toLowerCase(), rest: body.slice(run.length) };
}
