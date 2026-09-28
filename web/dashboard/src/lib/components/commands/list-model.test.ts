// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { CommandView } from '@bagel/kit';
import { listCommands, stateCounts } from './list-model';

const items: CommandView[] = [
  { name: 'discord', response: 'join us', is_active: true, perm: 'everyone', uses: '5' },
  { name: 'lurk', response: 'enjoy', is_active: false, perm: 'sub', uses: '9', aliases: ['bbl'] },
  { name: 'uptime', response: 'up', is_active: true, perm: 'everyone', builtin: true, uses: '1' }
];
const base = { state: 'all', perm: 'all', sort: 'uses', search: '' } as const;
const names = (rows: CommandView[]) => rows.map((c) => c.name);

describe('listCommands', () => {
  test('sorts by uses then name', () => {
    expect(names(listCommands(items, base))).toEqual(['lurk', 'discord', 'uptime']);
  });

  test('sorts by name', () => {
    expect(names(listCommands(items, { ...base, sort: 'name' }))).toEqual(['discord', 'lurk', 'uptime']);
  });

  test('filters by permission and state together', () => {
    expect(names(listCommands(items, { ...base, perm: 'everyone', state: 'custom' }))).toEqual(['discord']);
  });

  test('search matches aliases', () => {
    expect(names(listCommands(items, { ...base, search: 'bbl' }))).toEqual(['lurk']);
  });

  test('keep retains a just-toggled row outside the state filter', () => {
    const rows = listCommands(items, { ...base, state: 'active', keep: new Set(['lurk']) });
    expect(names(rows)).toEqual(['lurk', 'discord', 'uptime']);
  });
});

describe('stateCounts', () => {
  test('counts per state ignoring the state filter itself', () => {
    expect(stateCounts(items, { perm: 'all', search: '' })).toEqual({ all: 3, active: 2, disabled: 1, builtin: 1, custom: 2 });
  });

  test('respects permission and search scope', () => {
    expect(stateCounts(items, { perm: 'sub', search: '' }).all).toBe(1);
  });
});
