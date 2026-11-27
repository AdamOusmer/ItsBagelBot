// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, sep } from 'node:path';

const KIT_LIB = fileURLToPath(new URL('../lib/', import.meta.url));

/**
 * @param {string} appRoot
 * @returns {import('vite').Plugin}
 */
export function devFixtures(appRoot) {
  const appLib = join(appRoot, 'src', 'lib') + sep;
  const devRoot = join(appRoot, 'src', 'dev') + sep;

  /** @param {string} id */
  function overrideFor(id) {
    if (id.startsWith(appLib)) return devRoot + id.slice(appLib.length);
    if (id.startsWith(KIT_LIB)) return devRoot + 'kit' + sep + id.slice(KIT_LIB.length);
    return null;
  }

  /**
   * @param {string | null} override
   * @param {string} importer
   */
  function servable(override, importer) {
    if (!override || override === importer.split('?')[0]) return false;
    return existsSync(override);
  }

  return {
    name: 'bagel-dev-fixtures',
    enforce: 'pre',
    apply: (_config, { command, mode }) => command === 'serve' && mode === 'development',
    async resolveId(source, importer, options) {
      if (!importer || source.startsWith('\0')) return null;
      const resolved = await this.resolve(source, importer, { ...options, skipSelf: true });
      if (!resolved || resolved.external) return null;
      const override = overrideFor(resolved.id.split('?')[0]);
      return servable(override, importer) ? override : null;
    }
  };
}
