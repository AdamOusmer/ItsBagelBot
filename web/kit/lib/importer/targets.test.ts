// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { emit, normalizeInstant, positional, slice } from './targets';

describe('normalizeInstant', () => {
  const GROUPS: [name: string, pairs: [input: string, want: string | null][]][] = [
    ['RFC3339 passes through unchanged, calendar validated', [
      ['2026-12-25T05:00:00Z', '2026-12-25T05:00:00Z'],
      ['2026-12-25T05:00:00.000+02:00', '2026-12-25T05:00:00.000+02:00'],
      ['2026-02-30T00:00:00Z', null]
    ]],
    ['bare YYYY-MM-DD passes through, calendar validated', [
      ['2026-01-01', '2026-01-01'],
      ['2026-02-30', null]
    ]],
    ['a month-name date with a required zone normalizes to RFC3339 UTC', [
      ['Dec 25 2026 12:00:00 AM EST', '2026-12-25T05:00:00.000Z'],
      ['Jan 1 2026 00:00:00 UTC', '2026-01-01T00:00:00.000Z'],
      ['Jul 4 2026 3:00:00 PM PDT', '2026-07-04T22:00:00.000Z'],
      ['Dec 25 2026 EST', '2026-12-25T05:00:00.000Z']
    ]],
    ['a slash-separated MM/DD/YYYY date with a required zone normalizes', [
      ['12/25/2026 12:00:00 AM EST', '2026-12-25T05:00:00.000Z'],
      ['01/01/2026 UTC', '2026-01-01T00:00:00.000Z']
    ]],
    ['a date with no zone at all is refused, never read in a local zone', [
      ['Dec 25 2026 12:00:00 AM', null],
      ['12/25/2026', null]
    ]],
    ['an unrecognized zone abbreviation is refused rather than guessed', [
      ['Dec 25 2026 12:00:00 CET', null],
      ['Dec 25 2026 12:00:00 BST', null]
    ]],
    ['a bare time with no calendar date is refused, not anchored to today', [
      ['5:00:00 PM EST', null],
      ['17:00 UTC', null]
    ]],
    ['garbage small integers Date.parse would misread as years are refused', [
      ['0', null],
      ['1', null]
    ]],
    ['unrelated free text is refused', [
      ['whenever', null],
      ['', null]
    ]]
  ];

  test.each(GROUPS)('%s', (_name, pairs) => {
    for (const [input, want] of pairs) expect(normalizeInstant(input)).toBe(want);
  });
});

describe('positional/slice family', () => {
  test('positional mints {n} within range, null outside it', () => {
    expect(positional(1)).toBe('{1}');
    expect(positional(30)).toBe('{30}');
    expect(positional(31)).toBeNull();
    expect(positional(0)).toBeNull();
  });

  test('slice mints {n:}/{:m}/{n:m}', () => {
    expect(slice(2)).toBe('{2:}');
    expect(slice(undefined, 5)).toBe('{:5}');
    expect(slice(2, 5)).toBe('{2:5}');
    expect(slice()).toBeNull();
  });

  test('positional and slice accept an optional fallback', () => {
    expect(positional(1, 'everyone')).toBe('{1|everyone}');
    expect(slice(2, undefined, 'nothing')).toBe('{2:|nothing}');
    expect(positional(1, 'a|b')).toBeNull();
    expect(slice(2, undefined, 'a}b')).toBeNull();
  });
});

describe('emit', () => {
  test('emit mints a plain concept span', () => {
    expect(emit('user')).toBe('{user}');
    expect(emit('urlfetch', 'weather')).toBe('{urlfetch:weather}');
  });
});
