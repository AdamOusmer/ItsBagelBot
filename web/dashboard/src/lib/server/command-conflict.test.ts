// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { CommandView } from '@bagel/kit';
import { saveConflict } from './command-conflict';

const cmd = (name: string, aliases: string[] = []): CommandView => ({ name, aliases, response: 'x', is_active: true });
const existing = [cmd('discord', ['dc']), cmd('lurk')];
const create = (name: string, aliases: string[] = []) => ({ name, aliases, isEdit: false, originalName: '' });
const edit = (name: string, originalName: string, aliases: string[] = []) => ({ name, aliases, isEdit: true, originalName });

describe('saveConflict', () => {
  test('create with a free name passes', () => {
    expect(saveConflict(create('socials'), existing)).toBeNull();
  });

  test('create over an existing custom name is refused', () => {
    expect(saveConflict(create('discord'), existing)).toEqual({ code: 'name_taken', name: 'discord' });
  });

  test('create named like another command alias is refused', () => {
    expect(saveConflict(create('dc'), existing)).toEqual({ code: 'name_taken', name: 'dc' });
  });

  test('create over a built-in name is refused', () => {
    expect(saveConflict(create('uptime'), existing)?.code).toBe('builtin_name');
  });

  test('plain edit keeps its own name and aliases', () => {
    expect(saveConflict(edit('discord', 'discord', ['dc']), existing)).toBeNull();
  });

  test('rename onto another command is refused', () => {
    expect(saveConflict(edit('lurk', 'discord', ['dc']), existing)).toEqual({ code: 'name_taken', name: 'lurk' });
  });

  test('rename to a free name passes', () => {
    expect(saveConflict(edit('chat', 'discord', ['dc']), existing)).toBeNull();
  });

  test('new alias clashing with another command is refused', () => {
    expect(saveConflict(edit('discord', 'discord', ['dc', 'lurk']), existing)).toEqual({ code: 'alias_taken', name: 'lurk' });
  });
});
