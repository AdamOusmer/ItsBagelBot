// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { commandContentSnapshot, overlayLiveActive, persistCommandActive } from './command-active';

describe('persistCommandActive', () => {
  interface Row {
    name: string;
    args: Parameters<typeof persistCommandActive>;
    want: boolean;
  }

  const ROWS: Row[] = [
    { name: 'create uses the draft checkbox (new commands default on)', args: [false, true, false], want: true },
    { name: 'create keeps an unchecked draft off', args: [false, false, true], want: false },
    { name: 'edit uses the live row, not the inspector snapshot (#221)', args: [true, true, false], want: false },
    { name: 'edit follows a live row that is on', args: [true, false, true], want: true },
    { name: 'edit falls back to an on draft when the live row is gone', args: [true, true, undefined], want: true },
    { name: 'edit falls back to an off draft when the live row is gone', args: [true, false, undefined], want: false }
  ];

  test.each(ROWS)('$name', ({ args, want }) => {
    expect(persistCommandActive(...args)).toBe(want);
  });
});

describe('overlayLiveActive', () => {
  test('replaces a stale snapshot and keeps the same object when already live', () => {
    const stale = { name: 'lurk', is_active: true };
    expect(overlayLiveActive(stale, false)).toEqual({ name: 'lurk', is_active: false });
    expect(overlayLiveActive(stale, true)).toBe(stale);
  });
});

describe('commandContentSnapshot', () => {
  test('ignores is_active so a live toggle is not unsaved work', () => {
    const a = { name: 'lurk', response: 'hi', is_active: true };
    const b = { name: 'lurk', response: 'hi', is_active: false };
    expect(commandContentSnapshot(a)).toBe(commandContentSnapshot(b));
    expect(commandContentSnapshot({ ...a, response: 'yo' })).not.toBe(commandContentSnapshot(a));
  });
});
