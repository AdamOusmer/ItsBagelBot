// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { readdirSync } from 'node:fs';
import { join } from 'node:path';
import { neighbours, resolvedSpaces, spaceOf } from './spaces';

const ROOT = join(import.meta.dir, '../content/docs');

const englishIds = readdirSync(ROOT, { recursive: true, encoding: 'utf8' })
  .filter((file) => file.endsWith('.md') && !file.startsWith('fr/'))
  .map((file) => file.replace(/\.md$/, '').replace(/\/index$/, ''));

const spaces = resolvedSpaces(englishIds);
const listed = spaces.flatMap(({ groups }) => groups.flatMap(({ pages }) => pages));

test('every page sits in exactly one space', () => {
  expect([...listed].sort()).toEqual([...englishIds].sort());
});

test('a page outside every space fails the build', () => {
  expect(() => spaceOf('nowhere/at-all', spaces)).toThrow('belongs to no space');
});

test('neighbours stay inside the space and cross its groups', () => {
  const records = spaceOf('qa', spaces);
  const lastAdr = records.groups[0].pages.at(-1)!;
  expect(neighbours('qa', records)).toEqual({ prev: lastAdr, next: 'qa/monkey' });
  expect(neighbours('adr', records).prev).toBeUndefined();
});
