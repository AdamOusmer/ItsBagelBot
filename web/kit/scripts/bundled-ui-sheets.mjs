// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync, realpathSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';

const EMPTY = '\0bagel-bundled-ui-sheet';

/** @param {string | undefined} importer */
const fromScript = (importer) => !!importer && !importer.split('?')[0].endsWith('.css');

/** @param {string} path @param {Set<string>} into */
function collect(path, into) {
  for (const [, ref] of readFileSync(path, 'utf8').matchAll(/@import\s+'(\.\.?\/[^']+\.css)'/g)) {
    const nested = resolve(dirname(path), ref);
    into.add(nested);
    collect(nested, into);
  }
  return into;
}

// Adapter imports of element sheets load empty: app.css carries them once, in elements.css cascade order.
/** @returns {import('vite').Plugin} */
export function bundledUiSheets() {
  const entry = realpathSync(createRequire(import.meta.url).resolve('@bagel/ui/styles/elements.css'));
  const bundled = collect(entry, new Set());
  return {
    name: 'bagel-bundled-ui-sheets',
    enforce: 'pre',
    /** @param {string} source @param {string | undefined} importer */
    async resolveId(source, importer) {
      if (!source.endsWith('.css') || !fromScript(importer)) return null;
      const resolved = await this.resolve(source, importer, { skipSelf: true });
      return resolved && bundled.has(resolved.id) ? EMPTY : null;
    },
    /** @param {string} id */
    load(id) {
      return id === EMPTY ? 'export {};' : null;
    }
  };
}
