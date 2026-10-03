// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { CommandView } from '@bagel/kit';
import { hasCreatedAt, listCommands, stateCounts } from './list-model';

const items: CommandView[] = [
  { name: 'discord', response: 'join us', is_active: true, perm: 'everyone', uses: '5' },
  { name: 'lurk', response: 'enjoy', is_active: false, perm: 'sub', uses: '9', aliases: ['bbl'] },
  { name: 'uptime', response: 'up', is_active: true, perm: 'everyone', builtin: true, uses: '1' }
];
const dated = [{ ...items[0], created_at: 100 }, { ...items[1], created_at: 300 }, items[2]];
const base = { state: 'all', perm: 'all', sort: 'uses', search: '' } as const;

const lists: { name: string; rows: CommandView[]; view: object; want: string[] }[] = [
  { name: 'sorts by uses then name', rows: items, view: {}, want: ['lurk', 'discord', 'uptime'] },
  { name: 'sorts by name', rows: items, view: { sort: 'name' }, want: ['discord', 'lurk', 'uptime'] },
  { name: 'sorts recently added first, undated rows last', rows: dated, view: { sort: 'recent' }, want: ['lurk', 'discord', 'uptime'] },
  { name: 'filters by permission and state together', rows: items, view: { perm: 'everyone', state: 'custom' }, want: ['discord'] },
  { name: 'search matches aliases', rows: items, view: { search: 'bbl' }, want: ['lurk'] },
  { name: 'keep retains a just-toggled row outside the state filter', rows: items, view: { state: 'active', keep: new Set(['lurk']) }, want: ['lurk', 'discord', 'uptime'] }
];

describe('command list model', () => {
  test.each(lists)('listCommands $name', ({ rows, view, want }) => {
    expect(listCommands(rows, { ...base, ...view }).map((c) => c.name)).toEqual(want);
  });

  test('hasCreatedAt reports whether any row carries a creation time', () => {
    expect([hasCreatedAt(dated), hasCreatedAt(items)]).toEqual([true, false]);
  });

  test.each([
    { name: 'counts per state ignoring the state filter itself', scope: { perm: 'all', search: '' }, want: { all: 3, active: 2, disabled: 1, builtin: 1, custom: 2 } },
    { name: 'respects permission and search scope', scope: { perm: 'sub', search: '' }, want: { all: 1, active: 0, disabled: 1, builtin: 0, custom: 1 } }
  ] as const)('stateCounts $name', ({ scope, want }) => {
    expect(stateCounts(items, scope)).toEqual(want);
  });
});
