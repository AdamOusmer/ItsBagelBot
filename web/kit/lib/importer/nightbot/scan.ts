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

function split(body: string): { head: string; rest: string } {
  const run = NAME_RUN.exec(body)?.[0] ?? '';
  return { head: run.toLowerCase(), rest: body.slice(run.length) };
}

function matchParen(text: string, start: number): number {
  let depth = 0;
  for (let j = start + 1; j < text.length; j++) {
    if (text[j] === '(') depth++;
    else if (text[j] === ')' && --depth === 0) return j + 1;
  }
  return -1;
}
