// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { DEFAULT_MODULE_FLAGS, DEMO_MODULE_FLAGS, flagsFromRows } from './module-flags';

describe('flagsFromRows([]) — the "no rows at all" reading', () => {
  test('an opt-in module with no row is off (OptInView: missing row = off)', () => {
    expect(flagsFromRows([])['quotes']).toBe(false);
    expect(flagsFromRows([])['time']).toBe(false);
    expect(flagsFromRows([])['songqueue']).toBe(false);
    expect(flagsFromRows([])['loyalty']).toBe(false);
  });

  test('a gated built-in with no row is on (BuiltinEnabled: absent is ModuleOn)', () => {
    for (const id of ['followage', 'accountage', 'uptime', 'title', 'game']) {
      expect(flagsFromRows([])[id]).toBe(true);
    }
  });

  test('an explicit row always wins over the missing-row default, either polarity', () => {
    expect(flagsFromRows([{ name: 'quotes', is_enabled: true }])['quotes']).toBe(true);
    expect(flagsFromRows([{ name: 'followage', is_enabled: false }])['followage']).toBe(false);
  });

  test('DEFAULT_MODULE_FLAGS is exactly flagsFromRows([])', () => {
    expect(DEFAULT_MODULE_FLAGS).toEqual(flagsFromRows([]));
  });
});

describe('DEMO_MODULE_FLAGS', () => {
  test('every gate DEFAULT_MODULE_FLAGS names is true, both polarities alike', () => {
    expect(Object.keys(DEMO_MODULE_FLAGS).sort()).toEqual(Object.keys(DEFAULT_MODULE_FLAGS).sort());
    for (const v of Object.values(DEMO_MODULE_FLAGS)) expect(v).toBe(true);
  });
});
