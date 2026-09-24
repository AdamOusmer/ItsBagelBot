// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { emit, normalizeInstant, positional, slice } from './targets';

describe('normalizeInstant', () => {
  test('RFC3339 passes through unchanged, calendar validated', () => {
    expect(normalizeInstant('2026-12-25T05:00:00Z')).toBe('2026-12-25T05:00:00Z');
    expect(normalizeInstant('2026-12-25T05:00:00.000+02:00')).toBe('2026-12-25T05:00:00.000+02:00');
    expect(normalizeInstant('2026-02-30T00:00:00Z')).toBeNull();
  });

  test('bare YYYY-MM-DD passes through, calendar validated', () => {
    expect(normalizeInstant('2026-01-01')).toBe('2026-01-01');
    expect(normalizeInstant('2026-02-30')).toBeNull();
  });

  test('a month-name date with a required zone normalizes to RFC3339 UTC', () => {
    expect(normalizeInstant('Dec 25 2026 12:00:00 AM EST')).toBe('2026-12-25T05:00:00.000Z');
    expect(normalizeInstant('Jan 1 2026 00:00:00 UTC')).toBe('2026-01-01T00:00:00.000Z');
    expect(normalizeInstant('Jul 4 2026 3:00:00 PM PDT')).toBe('2026-07-04T22:00:00.000Z');
    expect(normalizeInstant('Dec 25 2026 EST')).toBe('2026-12-25T05:00:00.000Z');
  });

  test('a slash-separated MM/DD/YYYY date with a required zone normalizes', () => {
    expect(normalizeInstant('12/25/2026 12:00:00 AM EST')).toBe('2026-12-25T05:00:00.000Z');
    expect(normalizeInstant('01/01/2026 UTC')).toBe('2026-01-01T00:00:00.000Z');
  });

  test('a date with no zone at all is refused, never read in a local zone', () => {
    expect(normalizeInstant('Dec 25 2026 12:00:00 AM')).toBeNull();
    expect(normalizeInstant('12/25/2026')).toBeNull();
  });

  test('an unrecognized zone abbreviation is refused rather than guessed', () => {
    expect(normalizeInstant('Dec 25 2026 12:00:00 CET')).toBeNull();
    expect(normalizeInstant('Dec 25 2026 12:00:00 BST')).toBeNull();
  });

  test('a bare time with no calendar date is refused, not anchored to today', () => {
    expect(normalizeInstant('5:00:00 PM EST')).toBeNull();
    expect(normalizeInstant('17:00 UTC')).toBeNull();
  });

  test('garbage small integers Date.parse would misread as years are refused', () => {
    expect(normalizeInstant('0')).toBeNull();
    expect(normalizeInstant('1')).toBeNull();
  });

  test('unrelated free text is refused', () => {
    expect(normalizeInstant('whenever')).toBeNull();
    expect(normalizeInstant('')).toBeNull();
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
