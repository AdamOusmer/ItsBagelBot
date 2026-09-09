// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expand, lex, resolveToken, type VarToken } from './tmpl';

// The golden fixture is Go's, read at run time rather than imported, so the
// TypeScript build never has to reach outside console/ for a module and
// tsconfig's rootDir stays honest. Regenerating it is a Go-side flag
// (`go test ./pkg/tmpl/... -run TestTokenGolden -tmpl.write-golden`), never a
// side effect of running this suite: a fixture either language can rewrite
// documents whatever that language currently does, which is the opposite of
// the point of sharing one.
const GOLDEN_PATH = join(import.meta.dir, '../../../pkg/tmpl/testdata/tokens.golden.json');

interface GoldenRow {
  name: string;
  tmpl: string;
  values: Record<string, string>;
  want: string;
}

const golden: { note: string; rows: GoldenRow[] } = JSON.parse(readFileSync(GOLDEN_PATH, 'utf8'));

/** A row's values map as a resolver: an absent key is an unknown name, a key
 * mapped to "" is a name that resolved to nothing. */
function rowResolver(row: GoldenRow): (token: VarToken) => string | null {
  return (token) => (token.key in row.values ? row.values[token.key] : null);
}

describe('shared lexer golden (pkg/tmpl/testdata/tokens.golden.json)', () => {
  test('the fixture is present and non-trivial', () => {
    // A silently empty fixture would make every row below vacuously pass.
    expect(golden.rows.length).toBeGreaterThan(30);
  });

  for (const row of golden.rows) {
    test(row.name, () => {
      expect(expand(row.tmpl, rowResolver(row))).toBe(row.want);
    });
  }
});

describe('lex', () => {
  test('round-trips: text and raw concatenate back to the input', () => {
    const inputs = [
      '',
      'plain',
      '{user}',
      'hi {user}!',
      '{user}{title}',
      'a{b}c{user}d',
      '{user',
      '{{user}}',
      '{}',
      '}{user}',
      '{user}}',
      '{1|everyone}',
      '{choice:a|b|c}'
    ];
    for (const input of inputs) {
      const back = lex(input)
        .map((token) => (token.kind === 'literal' ? token.text : token.raw))
        .join('');
      expect(back).toBe(input);
    }
  });

  test('splits a span into a folded name, a case-kept payload and a fallback', () => {
    expect(lex('{User}')).toEqual([
      { kind: 'var', name: 'user', payload: null, fallback: null, raw: '{User}', key: 'user' }
    ]);
    expect(lex('{CHOICE:Hi,Yo}')).toEqual([
      {
        kind: 'var',
        name: 'choice',
        payload: 'Hi,Yo',
        fallback: null,
        raw: '{CHOICE:Hi,Yo}',
        key: 'choice:Hi,Yo'
      }
    ]);
    expect(lex('{so:Name|nobody}')).toEqual([
      {
        kind: 'var',
        name: 'so',
        payload: 'Name',
        fallback: 'nobody',
        raw: '{so:Name|nobody}',
        key: 'so:Name'
      }
    ]);
  });

  test('an absent payload is distinct from an empty one', () => {
    expect((lex('{choice}')[0] as VarToken).payload).toBeNull();
    expect((lex('{choice:}')[0] as VarToken).payload).toBe('');
  });

  test('an absent fallback is distinct from an empty one', () => {
    expect((lex('{1}')[0] as VarToken).fallback).toBeNull();
    expect((lex('{1|}')[0] as VarToken).fallback).toBe('');
  });

  test('the LAST pipe splits, so a payload keeps its own pipes', () => {
    const token = lex('{choice:a|b|c}')[0] as VarToken;
    expect(token.payload).toBe('a|b');
    expect(token.fallback).toBe('c');
  });

  test('interleaves literals and spans without empty filler', () => {
    expect(lex('a{user}{title}b')).toEqual([
      { kind: 'literal', text: 'a' },
      { kind: 'var', name: 'user', payload: null, fallback: null, raw: '{user}', key: 'user' },
      { kind: 'var', name: 'title', payload: null, fallback: null, raw: '{title}', key: 'title' },
      { kind: 'literal', text: 'b' }
    ]);
  });

  test('an unclosed brace opens no span', () => {
    expect(lex('oops {user')).toEqual([{ kind: 'literal', text: 'oops {user' }]);
  });
});

describe('resolveToken', () => {
  const token = lex('{1|everyone}')[0] as VarToken;

  test('an unknown name keeps its braces, fallback and all', () => {
    expect(resolveToken(token, null)).toBe('{1|everyone}');
  });

  test('an empty value renders the fallback', () => {
    expect(resolveToken(token, '')).toBe('everyone');
  });

  test('a value ignores the fallback', () => {
    expect(resolveToken(token, 'bob')).toBe('bob');
  });

  test('an empty value with no fallback renders nothing', () => {
    expect(resolveToken(lex('{1}')[0] as VarToken, '')).toBe('');
  });
});
