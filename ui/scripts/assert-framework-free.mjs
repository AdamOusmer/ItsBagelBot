#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile } from 'node:fs/promises';
import { builtinModules } from 'node:module';
import { dirname, join, relative, resolve, sep } from 'node:path';

const uiRoot = resolve(import.meta.dir, '..');

const SCAN_EXTENSIONS = new Set(['.ts', '.js', '.mjs', '.css', '.svelte', '.astro']);
const NODE_BUILTINS = new Set(builtinModules);

const isSvelte = (s) => s === 'svelte' || s.startsWith('svelte/');
const isAstro = (s) => s === 'astro' || s.startsWith('astro/') || s.startsWith('astro:');
const isSvelteKit = (s) => s.startsWith('$app/') || s.startsWith('@sveltejs/kit');
const isNode = (s) => s.startsWith('node:') || NODE_BUILTINS.has(s.split('/')[0]);

const EVERYWHERE = [
  { test: (s) => s.startsWith('@bagel/'), what: 'another @bagel package' },
  { test: (s) => s.startsWith('$lib') || s.startsWith('$env'), what: 'an app alias ($lib, $env)' },
];

const BANS = {
  lib: [
    { test: isSvelte, what: 'svelte' },
    { test: isAstro, what: 'astro' },
    { test: isSvelteKit, what: 'SvelteKit' },
    { test: isNode, what: 'a node builtin' },
  ],
  styles: [
    { test: isSvelte, what: 'svelte' },
    { test: isAstro, what: 'astro' },
  ],
  svelte: [
    { test: isAstro, what: 'astro' },
    { test: isSvelteKit, what: 'SvelteKit' },
    { test: isNode, what: 'a node builtin' },
  ],
  astro: [
    { test: isSvelte, what: 'svelte' },
    { test: isSvelteKit, what: 'SvelteKit' },
  ],
  types: [],
  scripts: [],
  test: [],
};

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

function escapesPackage(file, specifier) {
  if (!specifier.startsWith('.')) return false;
  const target = resolve(dirname(file), specifier);
  return target !== uiRoot && !target.startsWith(uiRoot + sep);
}

const SHIPPED = new Set(['lib', 'styles', 'svelte', 'astro', 'types']);

function problems(dir, file, specifier, bans) {
  const found = [...EVERYWHERE, ...bans].filter((ban) => ban.test(specifier)).map((ban) => ban.what);
  if (SHIPPED.has(dir) && escapesPackage(file, specifier)) found.push('a path outside @bagel/ui');
  return found;
}

const violations = [];

for (const [dir, bans] of Object.entries(BANS)) {
  for (const file of await walk(join(uiRoot, dir))) {
    const source = await readFile(file, 'utf8');
    for (const { specifier, line } of specifiers(source)) {
      for (const what of problems(dir, file, specifier, bans)) {
        violations.push(`${relative(uiRoot, file)}:${line}: imports ${what} ('${specifier}')`);
      }
    }
  }
}

if (violations.length > 0) {
  console.error('@bagel/ui has a forbidden dependency:\n');
  for (const v of violations) console.error(`  ✗ ${v}`);
  console.error(
    '\nlib/ and styles/ are framework-free; svelte/ and astro/ import only their own framework.' +
      '\nNothing in ui may import another @bagel package, an app alias, or a path outside ui:' +
      '\nthe dependency direction is apps -> kit -> ui -> nothing internal.',
  );
  process.exit(1);
}

console.log('✓ @bagel/ui dependencies point inward only (framework-free lib/ and styles/, no @bagel/*, no escapes)');
