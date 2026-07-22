#!/usr/bin/env node
// Regenerate lib/i18n/keys.d.ts from the English catalog. Run after adding or
// renaming keys in locales/en.json:
//   bun scripts/gen-i18n-keys.mjs   (or: bun run i18n:keys)
//
// fs-only, no app imports. The output is a committed source file, NOT a build
// step: it exists purely to give editors autocomplete + typo hints on t() keys.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const EN = join(here, '../lib/i18n/locales/en.json');
const OUT = join(here, '../lib/i18n/keys.d.ts');

// Depth-first collect of every leaf dot-path (string or string[] leaf).
function leafPaths(tree, prefix, out) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string' || Array.isArray(value)) out.push(path);
    else leafPaths(value, path, out);
  }
  return out;
}

const en = JSON.parse(readFileSync(EN, 'utf8'));
const keys = leafPaths(en, '', []).sort();
const union = keys.map((k) => `  | '${k}'`).join('\n');

const body = `// AUTO-GENERATED from lib/i18n/locales/en.json by scripts/gen-i18n-keys.mjs.
// Do not edit by hand. Regenerate after changing en.json:
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

writeFileSync(OUT, body);
console.log(`gen-i18n-keys: wrote ${keys.length} keys to ${OUT}`);
