#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

const uiRoot = resolve(import.meta.dir, '..');
const ADAPTERS = [
  ['svelte', /\.svelte$/],
  ['astro', /\.astro$/],
];

const RENDERS_CONTRACT = /(?<![\w-])bb-[a-z][a-z0-9_-]*/;
const IMPORTS_STYLES = /import\s+['"]\.\.\/styles\/[^'"]+\.css['"]/;

const bare = [];
for (const [dir, ext] of ADAPTERS) {
  for (const name of readdirSync(join(uiRoot, dir)).sort()) {
    if (!ext.test(name)) continue;
    const source = readFileSync(join(uiRoot, dir, name), 'utf8');
    if (RENDERS_CONTRACT.test(source) && !IMPORTS_STYLES.test(source)) bare.push(`${dir}/${name}`);
  }
}

if (bare.length > 0) {
  console.error('These adapters render bb-* classes but import no stylesheet:\n');
  for (const f of bare) console.error(`  ✗ ${f}`);
  console.error(
    "\nAn adapter must import the CSS of every contract it renders, or it only looks right" +
      "\nin an app that happened to load that CSS globally. Add `import '../styles/…css';`.",
  );
  process.exit(1);
}

console.log('every adapter that renders a bb-* contract imports its stylesheet');
