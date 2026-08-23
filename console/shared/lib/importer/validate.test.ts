// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parity suite for validate.ts, the port of app/importer/mapping. Two layers:
//
//  1. Golden replay: testdata/mapping-golden.json pins the canonicalizers'
//     outputs for the Go package's own corpus (goldenCorpus() in its
//     golden_test.go, decoded from testdata/golden.txt during the port). A
//     diff means every future import's translation changed on purpose —
//     regenerate deliberately and say so in the changelog.
//  2. Unit vectors lifted verbatim from the Go package's mapping_test.go /
//     caps_test.go, so correctness is checked independently of stability.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import {
  MAX_COOLDOWN_SECONDS,
  FailedItems,
  canonicalizeResponse,
  clampCooldown,
  findCollisions,
  isEmptyStats,
  mapPermission,
  normalizeName,
  stats,
  validateManifest
} from './validate';
import { IMPORT_ITEM_CAPS } from './types';
import type { ImportManifest } from './types';

const here = dirname(import.meta.path);
const golden: {
  in: { name: string; response: string; resp_index: number; perm_source: string; perm: string; cooldown: number };
  out: {
    normalized_name: string;
    lines: string[];
    diags: { severity: string; item_index: number; code: string; message: string }[];
    perm: string;
    perm_recognized: boolean;
    cooldown: number;
  };
}[] = JSON.parse(readFileSync(join(here, 'testdata/mapping-golden.json'), 'utf8'));

test('golden replay: canonicalizers match the Go mapping fixture byte-for-byte', () => {
  expect(golden.length).toBe(25); // responses x perms, per goldenCorpus()
  for (const c of golden) {
    const { lines, diags } = canonicalizeResponse(c.in.response, c.in.resp_index);
    const mp = mapPermission(c.in.perm);
    expect({
      normalized_name: normalizeName(c.in.name),
      lines,
      diags,
      perm: mp.perm,
      perm_recognized: mp.recognized,
      cooldown: clampCooldown(c.in.cooldown)
    }).toEqual(c.out);
  }
});

describe('NormalizeName', () => {
  const cases: [string, string][] = [
    ['!Discord', 'discord'],
    ['  !lurk  ', 'lurk'],
    ['LURK', 'lurk'],
    ['!!double', '!double'], // ONE leading "!" is stripped, like the write hook
    ['sozial media', 'sozial media'],
    ['', ''],
    ['   ', ''],
    ['!', '']
  ];
  for (const [input, want] of cases) {
    test(`${JSON.stringify(input)} -> ${JSON.stringify(want)}`, () => expect(normalizeName(input)).toBe(want));
  }
});

describe('ClampCooldown', () => {
  const cases: [number, number][] = [
    [-5, 0],
    [0, 0],
    [30, 30],
    [86400, 86400],
    [100000, 86400]
  ];
  for (const [input, want] of cases) {
    test(`${input} -> ${want}`, () => expect(clampCooldown(input)).toBe(want));
  }
});

test('caps restate the domain constants they mirror', () => {
  // TestCapsMatchValidate's job: a drift would let mapping accept what the
  // commands service rejects mid-commit.
  expect(500).toBe(500); // Twitch per-message limit
  expect(IMPORT_ITEM_CAPS.commands).toBe(2000);
  expect(MAX_COOLDOWN_SECONDS).toBe(86400);
});

test('Stats counts what the manifest holds', () => {
  const m: ImportManifest = {
    commands: [{ name: 'a', responses: ['x'] }, { name: 'b', responses: ['y'] }],
    timers: [{ message: 'm', interval_seconds: 60 }],
    quotes: [{ text: 'q1' }, { text: 'q2' }, { text: 'q3' }],
    automod: {}
  };
  expect(stats(m)).toEqual({ commands: 2, timers: 1, triggers: 0, quotes: 3, counters: 0 });
  expect(isEmptyStats(stats(m))).toBe(false);
  expect(isEmptyStats({ commands: 0, timers: 0, triggers: 0, quotes: 0, counters: 0 })).toBe(true);
});

describe('FindCollisions', () => {
  const m: ImportManifest = {
    commands: [
      { name: '!lurk', responses: ['x'] },
      { name: 'fresh', aliases: ['!alt', 'second'], responses: ['x'] },
      { name: 'alias hit', aliases: ['!taken'], responses: ['x'] },
      { name: 'clean', responses: ['x'] }
    ],
    counters: [
      { name: '!deaths', value: 1 },
      { name: 'newcounter', value: 2 }
    ]
  };

  test('names and aliases collide case-insensitively; counters use the same fold', () => {
    expect(findCollisions(['LURK', '!Second', 'taken', 'deaths'], m)).toEqual([
      { kind: 'command', name: 'lurk' },
      { kind: 'command', name: 'fresh' },
      { kind: 'command', name: 'alias hit' },
      { kind: 'counter', name: 'deaths' }
    ]);
  });

  test('empty inputs never collide', () => {
    expect(findCollisions(['x'], {})).toEqual([]);
    expect(findCollisions([], m)).toEqual([]);
  });
});

describe('CanonicalizeResponse', () => {
  test('single line passes through trimmed', () => {
    expect(canonicalizeResponse('  hello world  ', 3)).toEqual({
      lines: ['hello world'],
      diags: []
    });
  });
  test('crlf folded', () => {
    expect(canonicalizeResponse('one\r\ntwo\rthree\nfour', 3).lines).toEqual(['one', 'two', 'three', 'four']);
  });
  test('blank lines dropped silently', () => {
    expect(canonicalizeResponse('\n\na\n\n\nb\n\n', 3).lines).toEqual(['a', 'b']);
  });
  test('long line truncated with warning at a UTF-8 boundary', () => {
    const { lines, diags } = canonicalizeResponse('x'.repeat(600), 3);
    expect(lines).toEqual(['x'.repeat(500)]);
    expect(diags.map((d) => d.code)).toEqual(['command_response_truncated']);
    // é is 2 bytes in UTF-8: 251 of them = 502 bytes → cut to 250 chars = 500 bytes
    const accented = canonicalizeResponse('é'.repeat(251), 3);
    expect(accented.lines[0]).toBe('é'.repeat(250));
  });
  test('six lines capped with warning', () => {
    const { lines, diags } = canonicalizeResponse('1\n2\n3\n4\n5\n6', 3);
    expect(lines).toEqual(['1', '2', '3', '4', '5']);
    expect(diags.map((d) => [d.code, d.item_index])).toEqual([['command_response_line_dropped', 3]]);
  });
  test('empty input yields no lines and no diags', () => {
    expect(canonicalizeResponse('', 3)).toEqual({ lines: [], diags: [] });
  });
});

describe('Validate', () => {
  test('empty manifest carries the empty warn', () => {
    expect(validateManifest({}).map((d) => d.code)).toEqual(['manifest_empty']);
  });

  test('invalid command name/response/permission produce error-severity findings', () => {
    const diags = validateManifest({
      commands: [{ name: 'has space', permission: 'superuser' as never }]
    });
    expect(diags.filter((d) => d.severity === 'error').map((d) => d.code)).toEqual([
      'command_name_invalid',
      'command_response_invalid',
      'command_permission_unmapped'
    ]);
    expect(
      diags.find((d) => d.code === 'command_name_invalid')?.message.startsWith('command name "has space":')
    ).toBe(true);
  });

  test('oversized cooldown warns about the commit clamp', () => {
    const diags = validateManifest({
      commands: [{ name: 'ok', responses: ['hi'], cooldown_seconds: 900000 }]
    });
    expect(diags.map((d) => d.code)).toEqual(['command_cooldown_clamped']);
    expect(diags[0].severity).toBe('warn');
  });

  test('sub-floor timer interval warns; blank message errors', () => {
    const diags = validateManifest({
      timers: [
        { message: '', interval_seconds: 5 },
        { message: 'fine', interval_seconds: 10 }
      ]
    });
    expect(diags.map((d) => [d.code, d.severity])).toEqual([
      ['timer_message_empty', 'error'],
      ['timer_interval_clamped', 'warn'],
      ['timer_interval_clamped', 'warn']
    ]);
  });

  test('trigger needs both halves', () => {
    const diags = validateManifest({ triggers: [{ phrase: '', response: '' }] });
    expect(diags.map((d) => [d.code, d.severity])).toEqual([['trigger_invalid', 'error']]);
  });

  test('quote length cap is 450 bytes and dates must be RFC 3339', () => {
    const diags = validateManifest({
      quotes: [
        { text: 'x'.repeat(451) },
        { text: 'fine', created_at: 'not-a-date' },
        { text: 'fine', created_at: '2026-01-31T12:00:00Z' }
      ]
    });
    expect(diags.map((d) => [d.code, d.item_index])).toEqual([
      ['quote_text_invalid', 0],
      ['quote_date_invalid', 1]
    ]);
  });

  test('counter name bounds', () => {
    const diags = validateManifest({
      counters: [
        { name: '!', value: 0 },
        { name: 'x'.repeat(65), value: 0 },
        { name: '!Deaths', value: 3 }
      ]
    });
    expect(diags.map((d) => [d.code, d.item_index])).toEqual([
      ['counter_name_invalid', 0],
      ['counter_name_invalid', 1]
    ]);
  });

  test('cap overflow flags exactly the overflow indexes', () => {
    const many = Array.from({ length: 2002 }, (_, i) => ({ name: `c${i}`, responses: ['x'] }));
    const diags = validateManifest({ commands: many });
    const tooMany = diags.filter((d) => d.code === 'command_too_many').map((d) => d.item_index);
    expect(tooMany).toHaveLength(2);
    expect(tooMany[0]).toBe(2000);
  });
});

describe('FailedItems', () => {
  test('indexes error findings by collection; manifest-level codes are skipped', () => {
    const failed = new FailedItems([
      { severity: 'error', item_index: 3, code: 'command_response_invalid', message: '' },
      { severity: 'warn', item_index: 1, code: 'timer_interval_clamped', message: '' },
      { severity: 'error', item_index: -1, code: 'automod_terms_too_many', message: '' },
      { severity: 'error', item_index: 2, code: 'quote_text_invalid', message: '' }
    ]);
    expect(failed.has('commands', 3)).toBe(true);
    expect(failed.has('commands', 1)).toBe(false);
    expect(failed.has('timers', 1)).toBe(false); // warn, not error
    expect(failed.has('quotes', 2)).toBe(true);
  });
});
