// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

let rows: { name: string; is_enabled: boolean }[] = [];
mock.module('./commands-store', () => ({ listModules: async () => rows }));

const { flagsFromRows, moduleFlags } = await import('./module-flags');

const flagsFor = async (stored: typeof rows) => {
  rows = stored;
  return moduleFlags('1');
};

describe('module flags with no stored rows', () => {
  test('a default module with no row is on (enabledByDefault: missing row = on)', () => {
    for (const id of ['alerts', 'automod', 'personality']) {
      expect(flagsFromRows([])[id]).toBe(true);
    }
  });

  test('an opt-in module with no row is off (OptInView: missing row = off)', async () => {
    const flags = await flagsFor([]);
    expect(['quotes', 'time', 'songqueue', 'loyalty'].map((id) => flags[id])).toEqual([false, false, false, false]);
  });

  test('a gated built-in with no row is on (BuiltinEnabled: absent is ModuleOn)', async () => {
    const flags = await flagsFor([]);
    expect(['followage', 'accountage', 'uptime', 'title', 'game'].map((id) => flags[id])).toEqual([true, true, true, true, true]);
  });

  test('an explicit row always wins over the missing-row default, either polarity', async () => {
    const flags = await flagsFor([
      { name: 'quotes', is_enabled: true },
      { name: 'followage', is_enabled: false }
    ]);
    expect([flags.quotes, flags.followage]).toEqual([true, false]);
  });
});
