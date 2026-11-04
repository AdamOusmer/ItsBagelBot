// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { comparePasses } from './compare';
import { eventKinds } from './event-kinds';
import { compilesAsRe2 } from './regex';
import { parseDefinition, validateName } from './validate';

const TESTDATA = join(import.meta.dir, '../../../../internal/domain/automation/testdata');

function fixtures(dir: string): string[] {
  return readdirSync(join(TESTDATA, dir))
    .filter((f) => f.endsWith('.json') && !f.endsWith('.issues.json'))
    .sort();
}

const read = (...parts: string[]) => readFileSync(join(TESTDATA, ...parts), 'utf8');

describe('valid fixtures', () => {
  for (const file of fixtures('valid')) {
    test(file, () => {
      expect(parseDefinition(read('valid', file)).issues).toEqual([]);
    });
  }
});

describe('invalid fixtures', () => {
  for (const file of fixtures('invalid')) {
    test(file, () => {
      const golden = JSON.parse(read('invalid', file.replace(/\.json$/, '.issues.json')));
      expect(parseDefinition(read('invalid', file)).issues).toEqual(golden);
    });
  }
});

describe('comparePasses', () => {
  const cases: { left: string; op: string; right: string; want: boolean }[] = JSON.parse(read('compare_cases.json'));
  for (const { left, op, right, want } of cases) {
    test(`${JSON.stringify(left)} ${op} ${JSON.stringify(right)}`, () => {
      expect(comparePasses(op, left, right)).toBe(want);
    });
  }
});

describe('compilesAsRe2', () => {
  const cases: { pattern: string; want: boolean }[] = JSON.parse(read('regex_cases.json'));
  for (const { pattern, want } of cases) {
    test(`${JSON.stringify(pattern)} -> ${want}`, () => {
      expect(compilesAsRe2(pattern)).toBe(want);
    });
  }
});

describe('parseDefinition', () => {
  test('returns the definition when the shape decodes', () => {
    const { definition, issues } = parseDefinition('{"version":1,"trigger":{"type":"manual"}}');
    expect(issues).toEqual([]);
    expect(definition?.trigger.type).toBe('manual');
  });

  test('rejects trailing data and unknown top-level fields like Go', () => {
    expect(parseDefinition('{"version":1,"trigger":{"type":"manual"}} {}').issues).toEqual([{ path: '', code: 'invalid_json' }]);
    expect(parseDefinition('{"version":1,"trigger":{"type":"manual"},"x":1}').issues).toEqual([{ path: '', code: 'unknown_field' }]);
  });

  test('rejects oversized input', () => {
    expect(parseDefinition(' '.repeat(32769)).issues).toEqual([{ path: '', code: 'too_long' }]);
  });

  test('rejects regexes RE2 cannot compile', () => {
    const raw = (name: string) => JSON.stringify({ version: 1, trigger: { type: 'command', params: { name, match: 'regex' } } });
    for (const bad of ['(?=a)b', '(a)\\1', '(?<!x)y', '(unclosed']) {
      expect(parseDefinition(raw(bad)).issues).toEqual([{ path: 'trigger.params.name', code: 'invalid_regex' }]);
    }
    expect(parseDefinition(raw('(?i)^!hi (?P<who>\\w+)$')).issues).toEqual([]);
  });
});

describe('validateName', () => {
  test('mirrors the Go name rule', () => {
    expect(validateName('  ok  ')).toEqual([]);
    expect(validateName('   ')).toEqual([{ path: 'name', code: 'required' }]);
    expect(validateName('x'.repeat(65))).toEqual([{ path: 'name', code: 'too_long' }]);
    expect(validateName('a\u0007b')).toEqual([{ path: 'name', code: 'invalid_format' }]);
  });
});

describe('event kinds', () => {
  test('match the Go golden', () => {
    expect(eventKinds()).toEqual(JSON.parse(read('event_kinds.golden.json')));
  });
});
