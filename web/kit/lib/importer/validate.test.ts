// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import {
  FailedItems,
  canonicalizeResponse,
  clampCooldown,
  fetchDefSlug,
  findCollisions,
  isEmptyStats,
  isValidFetchDefName,
  mapPermission,
  normalizeName,
  stats,
  validateManifest
} from './validate';
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
  expect(golden.length).toBe(25);
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
    ['!!double', '!double'],
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

test('Stats counts what the manifest holds', () => {
  const m: ImportManifest = {
    commands: [{ name: 'a', responses: ['x'] }, { name: 'b', responses: ['y'] }],
    timers: [{ message: 'm', interval_seconds: 60 }],
    quotes: [{ text: 'q1' }, { text: 'q2' }, { text: 'q3' }],
    automod: {}
  };
  expect(stats(m)).toEqual({ commands: 2, timers: 1, triggers: 0, quotes: 3 });
  expect(isEmptyStats(stats(m))).toBe(false);
  expect(isEmptyStats({ commands: 0, timers: 0, triggers: 0, quotes: 0 })).toBe(true);
});

describe('FindCollisions', () => {
  const m: ImportManifest = {
    commands: [
      { name: '!lurk', responses: ['x'] },
      { name: 'fresh', aliases: ['!alt', 'second'], responses: ['x'] },
      { name: 'alias hit', aliases: ['!taken'], responses: ['x'] },
      { name: 'clean', responses: ['x'] }
    ]
  };

  test('names and aliases collide case-insensitively', () => {
    expect(findCollisions(['LURK', '!Second', 'taken', 'deaths'], m)).toEqual([
      { kind: 'command', name: 'lurk' },
      { kind: 'command', name: 'fresh' },
      { kind: 'command', name: 'alias hit' }
    ]);
  });

  test('empty inputs never collide', () => {
    expect(findCollisions(['x'], {})).toEqual([]);
    expect(findCollisions([], m)).toEqual([]);
  });

  test('fetch-definition slugs collide as kind "fetch"', () => {
    const withFetches: ImportManifest = {
      commands: [{ name: 'weather', responses: ['{urlfetch:moobot_weather}'] }],
      fetches: [{ name: 'moobot_weather', source: 'moobot' }, { name: 'se_fresh', url: 'https://x.example', source: 'streamelements' }]
    };
    expect(findCollisions(['MOOBOT_WEATHER', 'chat'], withFetches)).toEqual([{ kind: 'fetch', name: 'moobot_weather' }]);
  });
});

describe('CanonicalizeResponse', () => {
  test.each([
    ['single line passes through trimmed', '  hello world  ', ['hello world']],
    ['crlf folded', 'one\r\ntwo\rthree\nfour', ['one', 'two', 'three', 'four']],
    ['blank lines dropped silently', '\n\na\n\n\nb\n\n', ['a', 'b']],
    ['empty input yields no lines and no diags', '', []]
  ])('%s', (_name, input, lines) => {
    expect(canonicalizeResponse(input, 3)).toEqual({ lines, diags: [] });
  });
  test('long line truncated with warning at a UTF-8 boundary', () => {
    const { lines, diags } = canonicalizeResponse('x'.repeat(600), 3);
    expect(lines).toEqual(['x'.repeat(500)]);
    expect(diags.map((d) => d.code)).toEqual(['command_response_truncated']);
    const accented = canonicalizeResponse('é'.repeat(251), 3);
    expect(accented.lines[0]).toBe('é'.repeat(250));
  });
  test('six lines capped with warning', () => {
    const { lines, diags } = canonicalizeResponse('1\n2\n3\n4\n5\n6', 3);
    expect(lines).toEqual(['1', '2', '3', '4', '5']);
    expect(diags.map((d) => [d.code, d.item_index])).toEqual([['command_response_line_dropped', 3]]);
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

  test('oversized per-user cooldown warns about the commit clamp', () => {
    const diags = validateManifest({
      commands: [{ name: 'ok', responses: ['hi'], cooldown_seconds: 5, user_cooldown_seconds: 900000 }]
    });
    expect(diags.map((d) => [d.severity, d.message])).toEqual([
      ['warn', 'per-user cooldown 900000s clamped to 86400s at commit']
    ]);
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

  test('timer gate fields must be non-negative finite numbers', () => {
    const diags = validateManifest({
      timers: [
        { message: 'ok', interval_seconds: 600, min_chat_lines: 5, chat_window_minutes: 5 },
        { message: 'neg', interval_seconds: 600, min_chat_lines: -1 },
        { message: 'nan', interval_seconds: 600, chat_window_minutes: Number.NaN }
      ]
    });
    expect(diags.map((d) => [d.code, d.severity, d.item_index])).toEqual([
      ['timer_gate_invalid', 'error', 1],
      ['timer_gate_invalid', 'error', 2]
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
    expect(failed.has('timers', 1)).toBe(false);
    expect(failed.has('quotes', 2)).toBe(true);
  });
});

describe('fetchDefSlug', () => {
  test('folds a command name into the def-name grammar', () => {
    expect(fetchDefSlug('se', 'weather')).toBe('se_weather');
    expect(fetchDefSlug('moobot', 'my-cool cmd!')).toBe('moobot_my_cool_cmd');
    expect(fetchDefSlug('nightbot', '  Weather  ')).toBe('nightbot_weather');
  });

  test('leaves room for a slot suffix and never ends on an underscore', () => {
    const slug = fetchDefSlug('nightbot', 'a'.repeat(64));
    expect(slug.length).toBeLessThanOrEqual(32 - 5);
    expect(`${slug}_2000`.length).toBeLessThanOrEqual(32);
    expect(isValidFetchDefName(`${slug}_2000`)).toBe(true);
    expect(fetchDefSlug('se', `${'b'.repeat(23)}-tail`).endsWith('_')).toBe(false);
  });

  test('isValidFetchDefName rejects what the service rejects', () => {
    expect(isValidFetchDefName('se_weather')).toBe(true);
    expect(isValidFetchDefName('se-weather')).toBe(false);
    expect(isValidFetchDefName('SE_weather')).toBe(false);
    expect(isValidFetchDefName('a'.repeat(33))).toBe(false);
    expect(isValidFetchDefName('')).toBe(false);
  });
});
