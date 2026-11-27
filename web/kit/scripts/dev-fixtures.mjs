// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, sep } from 'node:path';

const KIT_LIB = fileURLToPath(new URL('../lib/', import.meta.url));

/** @param {string} appRoot */
function overrideMap(appRoot) {
  return [
    { from: join(appRoot, 'src', 'lib', 'server') + sep, to: join(appRoot, 'src', 'dev', 'server') + sep },
    { from: KIT_LIB, to: join(appRoot, 'src', 'dev', 'kit') + sep }
  ];
}

/**
 * @param {{ from: string, to: string }[]} map
 * @param {string} path
 */
function overrideFor(map, path) {
  const entry = map.find(({ from }) => path.startsWith(from));
  return entry ? entry.to + path.slice(entry.from.length) : null;
}

/**
 * @param {string | null} override
 * @param {string} importer
 */
function servable(override, importer) {
  if (!override || override === importer.split('?')[0]) return false;
  return existsSync(override);
}

/** @param {string | undefined} importer @param {string} source */
const skipped = (importer, source) => !importer || source.startsWith('\0');

/**
 * @param {string} appRoot
 * @returns {import('vite').Plugin}
 */
export function devFixtures(appRoot) {
  const map = overrideMap(appRoot);
  return {
    name: 'bagel-dev-fixtures',
    enforce: 'pre',
    apply: (_config, { command, mode }) => command === 'serve' && mode === 'development',
    async resolveId(source, importer, options) {
      if (skipped(importer, source)) return null;
      const resolved = await this.resolve(source, importer, { ...options, skipSelf: true });
      if (!resolved || resolved.external) return null;
      const [path, query] = resolved.id.split('?');
      const override = overrideFor(map, path);
      if (!servable(override, /** @type {string} */ (importer))) return null;
      return query ? `${override}?${query}` : override;
    }
  };
}
