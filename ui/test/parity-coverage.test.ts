// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(import.meta.dir, '..');
const names = (dir: string, extension: string) =>
  readdirSync(join(root, dir))
    .filter((file) => file.endsWith(extension))
    .map((file) => file.slice(0, -extension.length));

const svelteNames = new Set(names('svelte', '.svelte'));
const dual = names('astro', '.astro').filter((name) => svelteNames.has(name)).sort();

const EXEMPT: Record<string, string> = {};

const testDir = join(root, 'test');
const suites = readdirSync(testDir)
  .filter((file) => file.endsWith('.ts') && file !== 'parity-coverage.test.ts')
  .map((file) => readFileSync(join(testDir, file), 'utf8'));
const fixtureDir = join(testDir, 'fixtures');
const fixtureSources = new Map(
  readdirSync(fixtureDir).map((file) => [file, readFileSync(join(fixtureDir, file), 'utf8')] as const),
);

function uses(source: string, path: string): boolean {
  const local = new RegExp(`import (\\w+) from '${path.replace(/[./]/g, '\\$&')}'`).exec(source)?.[1];
  return local !== undefined && source.split(new RegExp(`\\b${local}\\b`)).length > 2;
}

function importedTogether(name: string): boolean {
  return suites.some(
    (suite) => uses(suite, `../svelte/${name}.svelte`) && uses(suite, `../astro/${name}.astro`),
  );
}

function coveredByFixture(name: string): boolean {
  const twins = [...fixtureSources.keys()]
    .filter((file) => file.endsWith('.svelte'))
    .map((file) => file.slice(0, -'.svelte'.length))
    .filter((base) => fixtureSources.get(`${base}.astro`) !== undefined)
    .filter(
      (base) =>
        fixtureSources.get(`${base}.svelte`)!.includes(`svelte/${name}.svelte'`) &&
        fixtureSources.get(`${base}.astro`)!.includes(`astro/${name}.astro'`),
    );
  return twins.some((base) =>
    suites.some((suite) => uses(suite, `./fixtures/${base}.svelte`) && uses(suite, `./fixtures/${base}.astro`)),
  );
}

test('every component with a Svelte and an Astro adapter has a parity case', () => {
  const uncovered = dual.filter((name) => !(name in EXEMPT) && !importedTogether(name) && !coveredByFixture(name));
  expect(uncovered).toEqual([]);
});

test('exemptions name a real dual-adapter component and say why', () => {
  for (const [name, reason] of Object.entries(EXEMPT)) {
    expect(dual).toContain(name);
    expect(reason.length).toBeGreaterThan(20);
  }
});

test('the dual-adapter list is not empty', () => {
  expect(dual.length).toBeGreaterThan(90);
});
