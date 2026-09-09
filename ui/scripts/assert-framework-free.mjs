#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Ports-and-adapters guard for @bagel/ui, shaped like web/kit's
// assert-demo-gated.mjs: a source-level grep test that names the file and the
// line rather than a rule nobody runs.
//
// What it enforces, and why each one is a real failure and not a style rule:
//
//   1. `lib/` and `styles/` import no framework. These are the DOMAIN half of
//      the package: CSS contracts and vanilla-TS engines that Svelte, Astro and
//      (one day) a plain <script> all drive. The moment one of them imports
//      `svelte`, every consumer of that file needs Svelte installed to use it,
//      and the Astro adapter starts shipping a Svelte runtime to render a
//      <button>. `$app/*` is worse: it is SvelteKit, so it drags a router and
//      an app-level environment into a design primitive. `astro:*` is the
//      mirror image. `node:*` is banned because these modules run in a browser;
//      a node builtin here means the file is not a UI primitive at all.
//
//   2. NOTHING anywhere in ui imports `@bagel/kit`. The dependency direction is
//      ui -> nothing internal; kit -> ui. A single import the other way makes
//      the library un-extractable to its own repo (the stated reason it is a
//      standalone package at the repo root rather than a member of the web/
//      workspace) and creates an import cycle the bundlers resolve differently
//      for SSR and client.
//
// What it cannot enforce: a framework dependency smuggled in as a global
// (`window.__lenis`, a Svelte store reached through a DOM property) or a copied
// helper. Those are review's job. This catches the shape that actually happens,
// which is someone reaching for the framework's convenience while editing a
// file that looks like it belongs to it.
//
// Adapters (`svelte/`, `astro/`) are deliberately NOT scanned for rule 1: they
// exist precisely to import their framework.

import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const uiRoot = resolve(import.meta.dir, '..');

// Directories that must stay framework-free, and the directories scanned for
// the kit ban. `scripts/` and `test/` are ui's own tooling: they may use node
// builtins and may compile Svelte, so they are only checked for @bagel/kit.
const DOMAIN_DIRS = ['lib', 'styles'];
const ALL_DIRS = ['lib', 'styles', 'svelte', 'astro', 'scripts', 'test'];

const SCAN_EXTENSIONS = new Set([
  '.ts', '.js', '.mjs', '.css', '.svelte', '.astro',
]);

// Matched against the SPECIFIER of an import/@import/require, never against
// free text: `import type { Foo } from './x'` mentioning the word svelte in a
// comment is not a violation, and a comment explaining why svelte is banned
// would otherwise fail its own file (this one nearly did).
const DOMAIN_BANS = [
  { test: (s) => s === 'svelte' || s.startsWith('svelte/'), what: 'svelte' },
  { test: (s) => s.startsWith('$app/'), what: '$app/* (SvelteKit)' },
  { test: (s) => s.startsWith('astro:'), what: 'astro:*' },
  { test: (s) => s === 'astro' || s.startsWith('astro/'), what: 'astro' },
  { test: (s) => s.startsWith('node:'), what: 'node:* builtin' },
  { test: (s) => s.startsWith('@bagel/kit'), what: '@bagel/kit' },
];

const KIT_BAN = { test: (s) => s.startsWith('@bagel/kit'), what: '@bagel/kit' };

// `import x from 'y'` / `import 'y'` / `export … from 'y'` / `import('y')` /
// `require('y')` / `@import 'y'` / `@import url('y')`. One pattern per shape
// rather than one clever one, so a miss is obvious.
const SPECIFIER_PATTERNS = [
  /\bimport\s+[^'";]*?\bfrom\s*['"]([^'"]+)['"]/g,
  /\bimport\s*['"]([^'"]+)['"]/g,
  /\bexport\s+[^'";]*?\bfrom\s*['"]([^'"]+)['"]/g,
  /\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)/g,
  /\brequire\s*\(\s*['"]([^'"]+)['"]\s*\)/g,
  /@import\s+(?:url\()?\s*['"]([^'"]+)['"]/g,
];

/** Every scannable file under `dir`, recursively. Missing dir = no files. */
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

/** [{ specifier, line }] for every import-like specifier in `source`. */
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
