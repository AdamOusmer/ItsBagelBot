// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { intactSpanWithFallback } from '../engine/tmpl-fallback';
import { intactSpan, mappedSpans } from './validate';
import { translateTags } from './moobot/tags';
import type { TagContext } from './moobot/tags';
import type { ImportManifest } from './types';

const here = dirname(import.meta.path);

describe('intactSpan', () => {
  test('builds the two span shapes', () => {
    expect(intactSpan('user', null)).toBe('{user}');
    expect(intactSpan('1', null)).toBe('{1}');
    expect(intactSpan('urlfetch', 'se_weather')).toBe('{urlfetch:se_weather}');
    expect(intactSpan('choice', 'a,b,c')).toBe('{choice:a,b,c}');
  });

  test('refuses a payload the lexer would re-cut', () => {
    expect(intactSpan('choice', 'a,b|c')).toBeNull();
    expect(intactSpan('choice', 'a,b}c')).toBeNull();
    expect(intactSpan('counter', 'deaths|hi')).toBeNull();
  });

  test('refuses a name that is not a name', () => {
    expect(intactSpan('us er}', null)).toBeNull();
    expect(intactSpan('User', null)).toBeNull();
  });
});

describe('moobot random-text lists survive hostile option text', () => {
  const ctx = (): TagContext => ({ name: 'roll', randomTexts: [], fetchDefs: new Map() });

  function choiceFor(options: string[]): string {
    const c = ctx();
    c.randomTexts = [options.map((text) => ({ text }))];
    return translateTags('<random.text.1>', c).text;
  }

  test('a pipe or a brace is stripped, never allowed to end the span', () => {
    expect(choiceFor(['yes|no', 'maybe'])).toBe('{choice:yesno,maybe}');
    expect(choiceFor(['a}b', 'c'])).toBe('{choice:ab,c}');
    expect(mappedSpans(choiceFor(['a}b', 'c']))[0].payload).toBe('ab,c');
  });

  test('a comma still refuses the tag outright', () => {
    expect(choiceFor(['a,b', 'c'])).toBe('<random.text.1>');
  });
});

const GOLDENS = [
  'moobot-golden.json',
  'se-golden.json',
  'fossabot-golden.json',
  'slcb-golden.json',
  'wizebot-golden.json',
  'nightbot-golden.json'
];

interface GoldenCase {
  manifest?: ImportManifest | null;
}

function goldenCases(file: string): GoldenCase[] {
  const parsed = JSON.parse(readFileSync(join(here, 'testdata', file), 'utf8'));
  return Array.isArray(parsed) ? (parsed as GoldenCase[]) : [parsed as GoldenCase];
}

function mappedText(m: ImportManifest | null | undefined): string[] {
  if (!m) return [];
  return [
    ...(m.commands ?? []).flatMap((c) => c.responses ?? []),
    ...(m.timers ?? []).map((t) => t.message),
    ...(m.triggers ?? []).map((t) => t.response)
  ];
}

describe('every span in a mapped manifest round-trips through the lexer', () => {
  for (const file of GOLDENS) {
    test(file, () => {
      const texts = goldenCases(file).flatMap((c) => mappedText(c.manifest));
      expect(texts.length).toBeGreaterThan(0);
      for (const text of texts) {
        for (const span of mappedSpans(text)) {
          const rebuilt =
            span.fallback === null
              ? intactSpan(span.name, span.payload)
              : intactSpanWithFallback(span.name, span.payload, span.fallback);
          expect(rebuilt).toBe(span.raw);
        }
      }
    });
  }
});
