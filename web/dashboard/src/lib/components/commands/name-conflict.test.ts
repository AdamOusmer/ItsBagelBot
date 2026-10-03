// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { nameConflict } from './name-conflict';

const custom = ['discord', 'lurk'];
const create = (name: string) => ({ edit: false, name, originalName: '' });
const edit = (name: string, originalName: string) => ({ edit: true, name, originalName });

const cases: { name: string; draft: ReturnType<typeof create>; want: ReturnType<typeof nameConflict> }[] = [
  { name: 'a free name has no conflict', draft: create('socials'), want: null },
  { name: 'an empty name is left to required validation', draft: create('  '), want: null },
  { name: 'a new command named like an existing one conflicts, ignoring case and bang', draft: create('!Discord'), want: { kind: 'taken', name: 'discord' } },
  { name: 'a built-in name conflicts', draft: create('uptime'), want: { kind: 'builtin' } },
  { name: 'editing keeps its own name', draft: edit('lurk', 'lurk'), want: null },
  { name: 'renaming onto another command conflicts', draft: edit('lurk', 'discord'), want: { kind: 'taken', name: 'lurk' } }
];

test.each(cases)('nameConflict ', ({ draft, want }) => {
  expect(nameConflict(draft, custom)).toEqual(want);
});
