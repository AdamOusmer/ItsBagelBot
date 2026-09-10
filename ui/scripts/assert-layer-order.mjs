// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Every stylesheet that OPENS a bb layer must also declare the layer ORDER
// before it does.
//
// Layer order is fixed by the first `@layer a, b, c;` statement the browser
// sees, and layers.css exists to be that statement. It only wins when it is
// loaded first, and it stopped being loaded first the moment the Svelte
// adapters began JS-importing their own element CSS: a bundler is free to emit
// Nav.svelte's `import '../styles/elements/nav.css'` ahead of the app's
// entry stylesheet, at which point `bb.elements` is the first layer named and
// ranks BELOW `bb.base`. The console shipped that way -- `.bb-tab` lost its
// 11px mono to the `button { font: inherit }` reset in bb.base and rendered at
// the body's 16px DM Sans on the public commands page.
//
// The statement is idempotent, so the fix is to repeat it at the top of every
// layered file and to fail the build when a new one forgets.
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
