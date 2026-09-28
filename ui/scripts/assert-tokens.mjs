#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const uiRoot = resolve(import.meta.dir, '..');
const DIRS = ['styles', 'svelte', 'astro', 'lib'];
const EXT = /\.(css|svelte|astro|ts)$/;

function walk(dir) {
  return readdirSync(dir)
    .sort()
    .flatMap((name) => {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) return walk(path);
      return EXT.test(name) ? [path] : [];
    });
}

const files = DIRS.flatMap((dir) => walk(join(uiRoot, dir))).map((path) => ({
  path,
  source: readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//g, ''),
}));

const defined = new Set();
const DEFINITION = /(--bb-[a-z0-9-]+)\s*:|['"`](--bb-[a-z0-9-]+)['"`]|style:(--bb-[a-z0-9-]+)/g;
for (const { source } of files) {
  for (const m of source.matchAll(DEFINITION)) defined.add(m[1] ?? m[2] ?? m[3]);
}

const missing = [];
const BARE_USE = /var\(\s*(--bb-[a-z0-9-]+)\s*\)/g;
for (const { path, source } of files) {
  for (const m of source.matchAll(BARE_USE)) {
    if (defined.has(m[1])) continue;
    const line = source.slice(0, m.index).split('\n').length;
    missing.push(`${relative(uiRoot, path)}:${line}: ${m[1]}`);
  }
}

if (missing.length > 0) {
  console.error('These tokens are read without a fallback but no file in @bagel/ui defines them:\n');
  for (const m of missing) console.error(`  ✗ ${m}`);
  console.error(
    '\nA token only an app defines renders as nothing in every other app.' +
      '\nDefine it in styles/brand.css (primitive) or styles/semantic.css (role),' +
      '\nor give the var() a fallback if it is a per-instance hook.',
  );
  process.exit(1);
}

console.log(`every --bb-* token read without a fallback is defined in @bagel/ui (${defined.size} defined)`);
