// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { commandContentSnapshot, overlayLiveActive, persistCommandActive } from './command-active';

describe('persistCommandActive', () => {
  test('create uses the draft checkbox (new commands default on)', () => {
    expect(persistCommandActive(false, true, false)).toBe(true);
    expect(persistCommandActive(false, false, true)).toBe(false);
  });

  test('edit uses the live row, not the inspector snapshot (#221)', () => {
    // Disabled command, stale draft still checked → stay disabled.
    expect(persistCommandActive(true, true, false)).toBe(false);
    // Row toggle on while inspector still shows off → keep the toggle.
    expect(persistCommandActive(true, false, true)).toBe(true);
  });

  test('edit falls back to the draft when the live row is gone', () => {
    expect(persistCommandActive(true, true, undefined)).toBe(true);
    expect(persistCommandActive(true, false, undefined)).toBe(false);
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
