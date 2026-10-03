// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import type { CommandView } from '@bagel/kit';
import { saveConflict } from './command-conflict';

const cmd = (name: string, aliases: string[] = []): CommandView => ({ name, aliases, response: 'x', is_active: true });
const existing = [cmd('discord', ['dc']), cmd('lurk')];
const create = (name: string, aliases: string[] = []) => ({ name, aliases, isEdit: false, originalName: '' });
const edit = (name: string, originalName: string, aliases: string[] = []) => ({ name, aliases, isEdit: true, originalName });

const cases: { name: string; save: ReturnType<typeof create>; want: ReturnType<typeof saveConflict> }[] = [
  { name: 'create with a free name passes', save: create('socials'), want: null },
  { name: 'create over an existing custom name is refused', save: create('discord'), want: { code: 'name_taken', name: 'discord' } },
  { name: 'create named like another command alias is refused', save: create('dc'), want: { code: 'name_taken', name: 'dc' } },
  { name: 'create over a built-in name is refused', save: create('uptime'), want: { code: 'builtin_name', name: 'uptime' } },
  { name: 'plain edit keeps its own name and aliases', save: edit('discord', 'discord', ['dc']), want: null },
  { name: 'rename onto another command is refused', save: edit('lurk', 'discord', ['dc']), want: { code: 'name_taken', name: 'lurk' } },
  { name: 'rename to a free name passes', save: edit('chat', 'discord', ['dc']), want: null },
  { name: 'new alias clashing with another command is refused', save: edit('discord', 'discord', ['dc', 'lurk']), want: { code: 'alias_taken', name: 'lurk' } }
];

test.each(cases)('saveConflict ', ({ save, want }) => {
  expect(saveConflict(save, existing)).toEqual(want);
});
