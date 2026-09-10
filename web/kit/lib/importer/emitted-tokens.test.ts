// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The round-trip guard for tokens the importers MINT.
//
// An importer writes this bot's {…} grammar out of strings another product
// controls, so a source value carrying '|' or '}' can end a span early and
// leave a token that reads correctly on the review screen and resolves to
// something else in chat. Two layers here:
//
//  1. intactSpan's own contract — the one sanctioned way to build a span.
//  2. A corpus sweep: every {…} span in every committed golden manifest is
//     re-lexed with the SHIPPED lexer and must come back as the same single
//     token. A mapping that starts amputating spans fails here rather than in
//     somebody's chat.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
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
    // '|' would become the span's fallback: {choice:a,b|c} offers "a,b" and
    // prints "c" when it resolves empty, which is not the list we meant.
    expect(intactSpan('choice', 'a,b|c')).toBeNull();
    // '}' closes the span at the first one, amputating the rest.
    expect(intactSpan('choice', 'a,b}c')).toBeNull();
    expect(intactSpan('counter', 'deaths|hi')).toBeNull();
  });

  test('refuses a name that is not a name', () => {
    expect(intactSpan('us er}', null)).toBeNull();
    // A name the lexer lower-cases is not the name we asked for: emitting it
    // would make the round trip a lie even though the token works.
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
    // Every option still reaches the payload — the old emission cut the list
    // at the first '}' and lost "c" entirely.
    expect(mappedSpans(choiceFor(['a}b', 'c']))[0].payload).toBe('ab,c');
  });

  test('a comma still refuses the tag outright', () => {
    // The separator cannot be repaired: three options must not become four,
    // so the tag stays the source's own literal text.
    expect(choiceFor(['a,b', 'c'])).toBe('<random.text.1>');
  });
});

// --- corpus sweep ------------------------------------------------------------

// GOLDENS are the committed mapped outputs of every source that has one. Two
// file shapes exist (an array of cases, or one case) because each suite was
// ported from its own Go golden; both are read here rather than reshaped, so
// this guard costs those suites nothing.
const GOLDENS = [
  'moobot-golden.json',
  'se-golden.json',
  'fossabot-golden.json',
  'slcb-golden.json',
  'wizebot-golden.json'
];

interface GoldenCase {
  manifest?: ImportManifest | null;
}

function goldenCases(file: string): GoldenCase[] {
  const parsed = JSON.parse(readFileSync(join(here, 'testdata', file), 'utf8'));
  return Array.isArray(parsed) ? (parsed as GoldenCase[]) : [parsed as GoldenCase];
}

/** Every chat-bound string one mapped manifest carries. */
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
          // Re-emitting the token the lexer read must reproduce the exact
          // bytes in the manifest. Anything the grammar re-cut (a swallowed
          // fallback, an early close) fails this equality.
          expect(intactSpan(span.name, span.payload)).toBe(span.raw);
        }
      }
    });
  }
});
