#!/usr/bin/env node
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { readConsoleTree } from '../lib/i18n/tree-fs.ts';

const here = dirname(fileURLToPath(import.meta.url));
const OUT = join(here, '../lib/i18n/keys.d.ts');

function leafPaths(tree, prefix, out) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string' || Array.isArray(value)) out.push(path);
    else leafPaths(value, path, out);
  }
  return out;
}

const en = readConsoleTree('en');
const keys = leafPaths(en, '', []).sort();
const union = keys.map((k) => `  | '${k}'`).join('\n');

const body = `// AUTO-GENERATED from locales/en/console by scripts/gen-i18n-keys.mjs.
// Do not edit by hand. Regenerate after changing the English console catalog:
//   bun scripts/gen-i18n-keys.mjs   (or: bun run i18n:keys)
//
// Soft key typing: KnownMessageKey enumerates every English leaf so the
// component-facing t() offers autocomplete and surfaces typos in the editor,
// while MessageKey stays open via (string & {}) so dynamically built keys and
// not-yet-generated additions never hard-fail the type check.
export type KnownMessageKey =
${union};

export type MessageKey = KnownMessageKey | (string & {});
`;

if (process.argv.includes('--check')) {
  if (!existsSync(OUT) || readFileSync(OUT, 'utf8') !== body) {
    console.error('gen-i18n-keys: lib/i18n/keys.d.ts is stale; run `bun run i18n:keys` in web/kit');
    process.exit(1);
  }
  console.log(`gen-i18n-keys: ${keys.length} keys up to date`);
} else {
  writeFileSync(OUT, body);
  console.log(`gen-i18n-keys: wrote ${keys.length} keys to ${OUT}`);
}
