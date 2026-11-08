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

test('guide navigation crosses task groups while decisions remain isolated', () => {
  const guides = spaceOf('guides/testing', spaces);
  expect(neighbours('reference/sesame-module-builder', guides)).toEqual({ prev: 'guides/testing', next: 'infrastructure/observability' });
  expect(neighbours('guides/getting-started', guides).prev).toBeUndefined();
  const decisions = spaceOf('adr', spaces);
  expect(neighbours('adr', decisions).prev).toBeUndefined();
  expect(neighbours(decisions.groups[0].pages.at(-1)!, decisions).next).toBeUndefined();
});
