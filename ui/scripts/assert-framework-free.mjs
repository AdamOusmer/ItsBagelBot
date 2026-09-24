#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const uiRoot = resolve(import.meta.dir, '..');

const DOMAIN_DIRS = ['lib', 'styles'];
const ALL_DIRS = ['lib', 'styles', 'svelte', 'astro', 'scripts', 'test'];

const SCAN_EXTENSIONS = new Set([
  '.ts', '.js', '.mjs', '.css', '.svelte', '.astro',
]);

const DOMAIN_BANS = [
  { test: (s) => s === 'svelte' || s.startsWith('svelte/'), what: 'svelte' },
  { test: (s) => s.startsWith('$app/'), what: '$app/* (SvelteKit)' },
  { test: (s) => s.startsWith('astro:'), what: 'astro:*' },
  { test: (s) => s === 'astro' || s.startsWith('astro/'), what: 'astro' },
  { test: (s) => s.startsWith('node:'), what: 'node:* builtin' },
  { test: (s) => s.startsWith('@bagel/kit'), what: '@bagel/kit' },
];

const KIT_BAN = { test: (s) => s.startsWith('@bagel/kit'), what: '@bagel/kit' };

const SPECIFIER_PATTERNS = [
  /\bimport\s+[^'";]*?\bfrom\s*['"]([^'"]+)['"]/g,
  /\bimport\s*['"]([^'"]+)['"]/g,
  /\bexport\s+[^'";]*?\bfrom\s*['"]([^'"]+)['"]/g,
  /\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)/g,
  /\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)/g,
  /@import\s+(?:url\()?\s*['"]([^'"]+)['"]/g,
];

async function walk(dir) {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const out = [];
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue;
      out.push(...(await walk(full)));
    } else if (SCAN_EXTENSIONS.has(entry.name.slice(entry.name.lastIndexOf('.')))) {
      out.push(full);
    }
  }
  return out;
}

function specifiers(source) {
  const found = [];
  for (const pattern of SPECIFIER_PATTERNS) {
    pattern.lastIndex = 0;
    let match;
    while ((match = pattern.exec(source)) !== null) {
      const line = source.slice(0, match.index).split('\n').length;
      found.push({ specifier: match[1], line });
    }
  }
  return found;
}

const violations = [];

for (const dir of ALL_DIRS) {
  const domain = DOMAIN_DIRS.includes(dir);
  const bans = domain ? DOMAIN_BANS : [KIT_BAN];
  for (const file of await walk(join(uiRoot, dir))) {
    const source = await readFile(file, 'utf8');
    for (const { specifier, line } of specifiers(source)) {
      for (const ban of bans) {
        if (!ban.test(specifier)) continue;
        violations.push(
          `${relative(uiRoot, file)}:${line}: imports ${ban.what} ('${specifier}')`,
        );
      }
    }
  }
}

if (violations.length > 0) {
  console.error('@bagel/ui is not framework-free:\n');
  for (const v of violations) console.error(`  ✗ ${v}`);
  console.error(
    '\nlib/ and styles/ are the framework-free domain half of this package;' +
      '\nput the framework-specific part in ui/svelte/ or ui/astro/ instead.' +
      '\n@bagel/kit may never be imported from ui at all: the dependency' +
      '\ndirection is ui -> nothing internal, kit -> ui.',
  );
  process.exit(1);
}

console.log('✓ @bagel/ui is framework-free (lib/, styles/) and kit-free');
