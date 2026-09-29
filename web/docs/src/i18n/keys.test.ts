// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { readFlatCatalogs } from '@bagel/kit/i18n/fs';

const en = readFlatCatalogs('docs').en;
const DOCS = join(import.meta.dir, '../..');

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) return name === 'content' ? [] : sources(full);
    return /\.(astro|ts|js|mjs)$/.test(name) && !name.endsWith('.test.ts') ? [full] : [];
  });
}

test('every docs key used in code or the sidebar exists in locales/en/docs', () => {
  const files = [...sources(join(DOCS, 'src')), join(DOCS, 'astro.config.mjs')];
  const used = new Map<string, string>();
  for (const file of files) {
    for (const match of readFileSync(file, 'utf8').matchAll(/(?<![.\w])(?:t|sidebarGroup)\(\s*(['"])([^'"\n]+)\1/g)) {
      used.set(match[2], relative(DOCS, file));
    }
  }
  expect(used.size).toBeGreaterThan(0);
  expect([...used].filter(([key]) => !(key in en)).map(([key, file]) => `${key} (${file})`)).toEqual([]);
});
