// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseTimer } from './timers-parse';

function withTimer(overrides: Record<string, unknown>): string {
  return JSON.stringify({ id: '', message: 'hi', intervalSeconds: 600, enabled: true, ...overrides });
}

type Field = 'minChatLines' | 'chatWindowMinutes' | 'allowOffline' | 'maxFiresPerStream' | 'endsAt';

const fieldCases = (field: Field, pairs: [given: unknown, want: unknown][]) => pairs.map(([given, want]) => ({ field, given, want }));

const fields = [
  ...fieldCases('minChatLines', [[undefined, 0], [-5, 0], [0, 0], [1, 1], [50, 50], [100, 100], [101, 100], [1000, 100], ['not a number', 0]]),
  ...fieldCases('chatWindowMinutes', [[undefined, 5], [0, 1], [-3, 1], [1, 1], [30, 30], [60, 60], [61, 60], ['not a number', 5]]),
  ...fieldCases('allowOffline', [[true, true], [false, false], [undefined, false], ['true', false], [1, false]]),
  ...fieldCases('maxFiresPerStream', [[undefined, 0], [-1, 0], [0, 0], [3, 3], [100, 100], [250, 100], [Number.NaN, 0]]),
  ...fieldCases('endsAt', [
    ['', ''],
    [undefined, ''],
    ['whenever', ''],
    ['2026-09-30T14:30:00Z', '2026-09-30T14:30:00.000Z'],
    ['2020-01-01T00:00:00Z', '2020-01-01T00:00:00.000Z']
  ])
];

describe('parseTimer', () => {
  test.each([
    { name: 'malformed JSON', raw: 'not json' },
    { name: 'an empty message', raw: withTimer({ message: '  ' }) }
  ])('rejects $name', ({ raw }) => {
    expect(parseTimer(raw)).toBeNull();
  });

  test.each(fields)('$field given $given becomes $want', ({ field, given, want }) => {
    expect(parseTimer(withTimer({ [field]: given }))?.[field]).toEqual(want as never);
  });
});
