// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { nameConflict } from './name-conflict';

const custom = ['discord', 'lurk'];
const create = (name: string) => ({ edit: false, name, originalName: '' });

test('a free name has no conflict', () => {
  expect(nameConflict(create('socials'), custom)).toBeNull();
});

test('an empty name is left to required validation', () => {
  expect(nameConflict(create('  '), custom)).toBeNull();
});

test('a new command named like an existing one conflicts, ignoring case and bang', () => {
  expect(nameConflict(create('!Discord'), custom)).toEqual({ kind: 'taken', name: 'discord' });
});

test('a built-in name conflicts', () => {
  expect(nameConflict(create('uptime'), custom)).toEqual({ kind: 'builtin' });
});

test('editing keeps its own name', () => {
  expect(nameConflict({ edit: true, name: 'lurk', originalName: 'lurk' }, custom)).toBeNull();
});

test('renaming onto another command conflicts', () => {
  expect(nameConflict({ edit: true, name: 'lurk', originalName: 'discord' }, custom)).toEqual({ kind: 'taken', name: 'lurk' });
});
