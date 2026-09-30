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
  const scan = new TokenScan(text);
  for (let i = 0; i < text.length; i++) {
    const token = scan.read(i);
    if (token) yield token;
  }
}

// One response's delimiter stack and latest token opener travel together.
// Separate character dispatch from closing a group so malformed/nested groups
// take the same flat, constant-time path as ordinary leaves.
class TokenScan {
  private readonly opens: number[] = [];
  private lastTokenStart = -1;

  constructor(private readonly text: string) {}

  read(at: number): Token | null {
    if (this.text[at] === '(') return this.open(at);
    if (this.text[at] === ')') return this.close(at);
    return null;
  }

  private open(at: number): null {
    this.opens.push(at);
    if (this.text[at - 1] === '$') this.lastTokenStart = at - 1;
    return null;
  }

  private close(at: number): Token | null {
    const open = this.opens.pop();
    if (open === undefined) return null;
    const start = open - 1;
    if (this.text[start] !== '$') return null;
    if (this.lastTokenStart > start) return null;
    const end = at + 1;
    return { ...split(this.text.slice(open + 1, at).trim()), raw: this.text.slice(start, end), start, end };
  }
}

function split(body: string): { head: string; rest: string } {
  const run = NAME_RUN.exec(body)?.[0] ?? '';
  return { head: run.toLowerCase(), rest: body.slice(run.length) };
}
