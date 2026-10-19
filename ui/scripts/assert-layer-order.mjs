// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const ORDER = '@layer bb.tokens, bb.base, bb.elements, bb.app;';
const ROOT = new URL('../styles/', import.meta.url).pathname;

function walk(dir) {
  return readdirSync(dir)
    .sort()
    .flatMap((name) => {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) return walk(path);
      return name.endsWith('.css') ? [path] : [];
    });
}

function importsOf(path) {
  const css = readFileSync(path, 'utf8');
  return [...css.matchAll(/@import\s+'(\.\.?\/[^']+\.css)'/g)].map((m) => join(path, '..', m[1]));
}

function bundledSheets(path, into = new Set()) {
  for (const nested of importsOf(path)) {
    into.add(nested);
    bundledSheets(nested, into);
  }
  return into;
}

function checkSheet(path, bad) {
  const css = readFileSync(path, 'utf8');
  const opens = css.match(/@layer\s+bb\.[a-z]+\s*\{/);
  if (!opens) return;
  const order = css.indexOf(ORDER);
  if (order === -1 || order > css.indexOf(opens[0])) bad.push(`styles/${path.slice(ROOT.length)}: declare ${ORDER} above the first @layer block`);
}

function checkBundle(sheets, bad) {
  for (const path of walk(ROOT)) {
    if (/@layer\s+bb\.elements\s*\{/.test(readFileSync(path, 'utf8')) && !sheets.has(path)) {
      bad.push(`styles/elements.css: import styles/${path.slice(ROOT.length)}`);
    }
  }
}

const sheets = bundledSheets(join(ROOT, 'elements.css'));
const bad = [];
for (const path of walk(ROOT)) checkSheet(path, bad);
checkBundle(sheets, bad);

if (bad.length) {
  console.error(`Layer order problems:\n${bad.map((line) => `  ${line}`).join('\n')}\n`);
  process.exit(1);
}

console.log(`layer order declared in every layered stylesheet; ${sheets.size} element sheets bundled`);
