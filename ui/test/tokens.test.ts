// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const ROOT = join(import.meta.dir, '..');

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return walk(path);
    return /\.(css|svelte|astro)$/.test(name) ? [path] : [];
  });
}

const sources = ['styles', 'svelte', 'astro']
  .flatMap((dir) => walk(join(ROOT, dir)))
  .map((path) => ({ path: relative(ROOT, path), text: readFileSync(path, 'utf8') }));

test('every token ui reads without a fallback is declared inside ui', () => {
  const declared = new Set(sources.flatMap(({ text }) => [...text.matchAll(/(--bb-[\w-]+)\s*:/g)].map((m) => m[1])));
  const missing = sources.flatMap(({ path, text }) =>
    [...text.matchAll(/var\(\s*(--bb-[\w-]+)\s*\)/g)]
      .map((m) => m[1])
      .filter((name) => !declared.has(name))
      .map((name) => `${path}: ${name}`),
  );
  expect(missing).toEqual([]);
});

test('global tokens are declared only in brand.css', () => {
  const owners = sources
    .filter(({ path }) => path.startsWith('styles/') && path !== 'styles/brand.css')
    .filter(({ text }) => /(^|[\s,}]):root\s*\{/.test(text))
    .map(({ path }) => path);
  expect(owners).toEqual([]);
});
