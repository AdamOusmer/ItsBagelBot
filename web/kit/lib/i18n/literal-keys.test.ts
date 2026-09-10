// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Every t('some.key') literal in the console must resolve to a real string.
//
// Nothing enforced this before. keys.d.ts is generated from the locales and
// looks like a type guard, but translate() takes `key: string`, so a call site
// naming a key that does not exist type-checks fine and lookup() falls back to
// returning the key itself. The failure is therefore invisible in CI and
// visible only as raw "billing.premiumFeat5" text on the page, which is how
// exactly that shipped: a key was dropped from the locale while its call site
// stayed.
//
// Only literal keys are checked. Dynamic lookups (ERROR_SLUG_KEYS[slug] and
// friends) are indexed through typed maps whose VALUES are literals elsewhere
// in the source, so they are covered where they are declared rather than where
// they are used.

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

// t('a.b') / t("a.b") / t(`a.b`), optionally with params after the key.
const CALL = /\bt\(\s*['"`]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)['"`]/g;

// Every t() literal in the console resolves. This list is empty and should
// stay that way: a key belongs here only while its copy is genuinely
// undecided, and the test below fails the moment a listed key starts
// resolving, so it can never rot into a permanent excuse.
const KNOWN_MISSING = new Set<string>([]);

// One file's worth of unresolved keys. Split out of the test body because the
// scan is three nested loops (roots, files, matches) and CodeScene is right
// that reading a failure out of that is harder than it needs to be.
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

  // The baseline must shrink, never linger. Once a key is given a string, it
  // has to leave this list or the list stops meaning "known broken".
  test('the known-missing baseline holds no keys that now resolve', () => {
    const fixed = [...KNOWN_MISSING].filter(resolves);
    expect(fixed).toEqual([]);
  });

  // The key that motivated this test, pinned by name: it is a plan-comparison
  // bullet, so losing it again silently downgrades what Premium appears to buy.
  test('the premium feature list is complete', () => {
    for (const n of [1, 2, 3, 4, 5]) {
      expect(resolves(`billing.premiumFeat${n}`)).toBe(true);
    }
  });
});
