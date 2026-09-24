// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { extname, join } from 'node:path';
import en from './locales/en.json';

const ROOTS = [
  join(import.meta.dir, '../../../dashboard/src'),
  join(import.meta.dir, '../../../admin/src'),
  join(import.meta.dir, '../..')
];
const EXT = new Set(['.svelte', '.ts']);
const SKIP = new Set(['node_modules', '.svelte-kit', 'build', 'dist', 'locales']);

function sourceFiles(dir: string): string[] {
  let out: string[] = [];
  for (const name of readdirSync(dir)) {
    if (SKIP.has(name)) continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out = out.concat(sourceFiles(full));
    else if (EXT.has(extname(name)) && !full.endsWith('.test.ts')) out.push(full);
  }
  return out;
}

function resolves(key: string): boolean {
  let node: unknown = en;
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return false;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === 'string' || Array.isArray(node);
}

const CALL = /\bt\(\s*['"`]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)['"`]/g;

const KNOWN_MISSING = new Set<string>([]);

function missingKeysIn(file: string): string[] {
  const label = file.split('/src/').pop() ?? file;
  const out: string[] = [];
  for (const m of readFileSync(file, 'utf8').matchAll(CALL)) {
    const key = m[1];
    if (!resolves(key) && !KNOWN_MISSING.has(key)) out.push(`${key}  (${label})`);
  }
  return out;
}

const ALL_SOURCES = ROOTS.flatMap(sourceFiles);

describe('i18n literal keys', () => {
  test('every t() literal resolves in en.json', () => {
    expect(ALL_SOURCES.flatMap(missingKeysIn)).toEqual([]);
  });

  test('the known-missing baseline holds no keys that now resolve', () => {
    const fixed = [...KNOWN_MISSING].filter(resolves);
    expect(fixed).toEqual([]);
  });

  test('the premium feature list is complete', () => {
    for (const n of [1, 2, 3, 4, 5]) {
      expect(resolves(`billing.premiumFeat${n}`)).toBe(true);
    }
  });
});
