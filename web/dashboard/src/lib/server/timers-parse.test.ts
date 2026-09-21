// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseTimer } from './timers-parse';

function withTimer(overrides: Record<string, unknown>): string {
  return JSON.stringify({ id: '', message: 'hi', intervalSeconds: 600, enabled: true, ...overrides });
}

describe('parseTimer', () => {
  test('rejects malformed JSON', () => {
    expect(parseTimer('not json')).toBeNull();
  });

  test('rejects an empty message', () => {
    expect(parseTimer(withTimer({ message: '  ' }))).toBeNull();
  });

  test('defaults the three condition fields when absent (old blob)', () => {
    const draft = parseTimer(JSON.stringify({ id: '', message: 'hi', intervalSeconds: 600, enabled: true }));
    expect(draft).not.toBeNull();
    expect(draft!.minChatLines).toBe(0);
    expect(draft!.maxFiresPerStream).toBe(0);
    expect(draft!.endsAt).toBe('');
  });

  describe('minChatLines clamp (0-100, default 0)', () => {
    for (const [raw, want] of [
      [undefined, 0],
      [-5, 0],
      [0, 0],
      [1, 1],
      [50, 50],
      [100, 100],
      [101, 100],
      [1000, 100],
      ['not a number', 0]
    ] as const) {
      test(`${JSON.stringify(raw)} -> ${want}`, () => {
        expect(parseTimer(withTimer({ minChatLines: raw }))!.minChatLines).toBe(want);
      });
    }
  });

  describe('maxFiresPerStream clamp (0-100, default 0)', () => {
    for (const [raw, want] of [
      [undefined, 0],
      [-1, 0],
      [0, 0],
      [3, 3],
      [100, 100],
      [250, 100],
      [Number.NaN, 0]
    ] as const) {
      test(`${JSON.stringify(raw)} -> ${want}`, () => {
        expect(parseTimer(withTimer({ maxFiresPerStream: raw }))!.maxFiresPerStream).toBe(want);
      });
    }
  });

  describe('endsAt', () => {
    test('empty string stays empty', () => {
      expect(parseTimer(withTimer({ endsAt: '' }))!.endsAt).toBe('');
    });

    test('missing field stays empty', () => {
      expect(parseTimer(withTimer({}))!.endsAt).toBe('');
    });

    test('an unparsable string is dropped to empty', () => {
      expect(parseTimer(withTimer({ endsAt: 'whenever' }))!.endsAt).toBe('');
    });

    test('a parsable date string is stored as a UTC ISO instant', () => {
      const draft = parseTimer(withTimer({ endsAt: '2026-09-30T14:30:00Z' }));
      expect(draft!.endsAt).toBe('2026-09-30T14:30:00.000Z');
    });

    test('a past date is accepted as-is', () => {
      const draft = parseTimer(withTimer({ endsAt: '2020-01-01T00:00:00Z' }));
      expect(draft!.endsAt).toBe('2020-01-01T00:00:00.000Z');
    });
  });
});
