// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { queryEscape, resolveComputedUtil } from './pure';
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

describe('computed utilities beyond the Go golden', () => {
  const rows = [
    { name: 'math works past the double-precision range, like the int64 evaluator', tmpl: '{math:9007199254740993}', want: '9007199254740993' },
    { name: 'math keeps exactness up to the int64 ceiling', tmpl: '{math:4611686018427387903*2+1}', want: '9223372036854775807' },
    { name: 'math refuses the step that leaves the int64 range downward', tmpl: '{math:0-9223372036854775807-2}', want: '' },
    { name: 'repeat counts the byte length of the phrase, not its characters', tmpl: '{repeat:20:🥯}', want: Array(20).fill('🥯').join(' ') },
    { name: 'repeat refuses a phrase over the byte cap', tmpl: `{repeat:20:${'x'.repeat(24)}}`, want: '' }
  ];

  test.each(rows)('$name', ({ tmpl, want }) => {
    expect(expand(tmpl, resolveComputedUtil)).toBe(want);
  });
});

describe('the URL encoders', () => {
  test('encodeURIComponent is NOT the same function', () => {
    expect(queryEscape("!'()*")).toBe('%21%27%28%29%2A');
    expect(encodeURIComponent("!'()*")).toBe("!'()*");
  });

  test('non-ASCII is encoded from its UTF-8 bytes', () => {
    expect(queryEscape('café 🥯')).toBe('caf%C3%A9+%F0%9F%A5%AF');
  });
});
