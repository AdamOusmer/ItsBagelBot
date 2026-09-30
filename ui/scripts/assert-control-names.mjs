#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { findUnnamed } from './control-names.ts';

const repoRoot = resolve(import.meta.dir, '..', '..');
const webRoot = join(repoRoot, 'web');
const SKIPPED = new Set(['node_modules', 'dist', '.svelte-kit', '.astro', '.output']);
const SOURCE = /\.(svelte|astro)$/;

function* sources(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (SKIPPED.has(entry.name)) continue;
    const path = join(dir, entry.name);
    if (entry.isDirectory()) yield* sources(path);
    else if (SOURCE.test(entry.name)) yield path;
  }
}

if (!existsSync(webRoot)) {
  console.log('no web/ tree next to ui/, control-name scan skipped');
  process.exit(0);
}

const unnamed = [];
for (const file of sources(webRoot)) {
  for (const { component, line } of findUnnamed(readFileSync(file, 'utf8'))) {
    unnamed.push(`${relative(repoRoot, file)}:${line} <${component}>`);
  }
}

if (unnamed.length > 0) {
  console.error('These controls have no accessible name:\n');
  for (const site of unnamed) console.error(`  ✗ ${site}`);
  console.error(
    '\nWrap the control in <Field label="…"> or <label>, or pass aria-label, aria-labelledby or label.',
  );
  process.exit(1);
}

console.log('every Checkbox, Input, Select, Slider and Textarea call site has an accessible name');
