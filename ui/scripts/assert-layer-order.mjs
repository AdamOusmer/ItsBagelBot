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

const bad = [];
for (const path of walk(ROOT)) {
  const css = readFileSync(path, 'utf8');
  const opens = css.match(/@layer\s+bb\.[a-z]+\s*\{/);
  if (!opens) continue;
  const order = css.indexOf(ORDER);
  if (order === -1 || order > css.indexOf(opens[0])) {
    bad.push(path.slice(ROOT.length));
  }
}

if (bad.length) {
  console.error(
    `These stylesheets open a bb layer without declaring the layer order first:\n` +
      bad.map((f) => `  styles/${f}`).join('\n') +
      `\n\nAdd this line above the first @layer block:\n  ${ORDER}\n`
  );
  process.exit(1);
}

console.log(`layer order declared in every layered stylesheet`);
