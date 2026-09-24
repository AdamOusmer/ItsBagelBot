// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { evalMath, pathEscape, queryEscape, repeatPhrase, resolveComputedUtil } from './pure';
import { expand } from './tmpl';

const GOLDEN_PATH = join(
  import.meta.dir,
  '../../../../app/twitch/sesame/engine/scope/testdata/pure.golden.json'
);

interface GoldenRow {
  name: string;
  tmpl: string;
  want: string;
}

const golden: { note: string; rows: GoldenRow[] } = JSON.parse(readFileSync(GOLDEN_PATH, 'utf8'));

describe('pure utility golden (engine/scope/testdata/pure.golden.json)', () => {
  test('the fixture is present and non-trivial', () => {
    expect(golden.rows.length).toBeGreaterThan(50);
  });

  for (const row of golden.rows) {
    test(row.name, () => {
      expect(expand(row.tmpl, resolveComputedUtil)).toBe(row.want);
    });
  }
});

describe('evalMath', () => {
  test('works past the double-precision range, like the int64 evaluator', () => {
    expect(evalMath('9007199254740993')).toBe('9007199254740993');
    expect(evalMath('4611686018427387903*2+1')).toBe('9223372036854775807');
  });

  test('refuses the step that leaves the int64 range', () => {
    expect(evalMath('9223372036854775807+1')).toBe('');
    expect(evalMath('0-9223372036854775807-2')).toBe('');
  });

  test('an empty payload is not an expression', () => {
    expect(evalMath('')).toBe('');
  });
});

describe('the URL encoders', () => {
  test('query escape and path escape differ on a space', () => {
    expect(queryEscape('a b')).toBe('a+b');
    expect(pathEscape('a b')).toBe('a%20b');
  });

  test('encodeURIComponent is NOT the same function', () => {
    expect(queryEscape("!'()*")).toBe('%21%27%28%29%2A');
    expect(encodeURIComponent("!'()*")).toBe("!'()*");
  });

  test('non-ASCII is encoded from its UTF-8 bytes', () => {
    expect(queryEscape('café 🥯')).toBe('caf%C3%A9+%F0%9F%A5%AF');
  });
});

describe('repeatPhrase', () => {
  test('counts the byte length of the phrase, not its characters', () => {
    expect(repeatPhrase('20:🥯')).toContain('🥯');
    expect(repeatPhrase('20:' + 'x'.repeat(24))).toBe('');
  });

  test('the first colon splits, so a phrase keeps its own colons', () => {
    expect(repeatPhrase('2:a:b')).toBe('a:b a:b');
  });
});
