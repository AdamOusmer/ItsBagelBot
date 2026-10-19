// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const ROUTE_FILE = /\/src\/routes\/(?:.*\/)?\+[^/]+\.svelte$/;
const MERGEABLE = /\/ui\/(svelte|lib|locales)\/[^?]+\.(svelte|ts|js)$/;

/**
 * @typedef {{ importedIds: readonly string[], dynamicallyImportedIds: readonly string[] }} ModuleLinks
 * @param {{ getModuleIds(): Iterable<string>, getModuleInfo(id: string): ModuleLinks | null }} ctx
 * @param {(routeFile: string) => boolean} pick
 */
function reach(ctx, pick) {
  const seen = new Set([...ctx.getModuleIds()].filter((id) => ROUTE_FILE.test(id) && pick(id)));
  for (const id of seen) {
    const info = ctx.getModuleInfo(id);
    [...(info?.importedIds ?? []), ...(info?.dynamicallyImportedIds ?? [])].forEach((next) => seen.add(next));
  }
  return seen;
}

/**
 * Merges small client chunks of modules only console routes reach, so console pages make fewer requests
 * while public pages keep their default chunks.
 * @param {{ consoleRoutes: RegExp }} options
 * @returns {import('vite').Plugin}
 */
export function consoleChunks({ consoleRoutes }) {
  /** @type {Set<string>} */
  let publicModules = new Set();
  const consoleOnly = (/** @type {string} */ id) => MERGEABLE.test(id) && !publicModules.has(id);
  const group = {
    name: 'console',
    test: consoleOnly,
    includeDependenciesRecursively: false,
    entriesAware: true,
    entriesAwareMergeThreshold: 64 * 1024,
    priority: -1
  };
  return {
    name: 'bagel-console-chunks',
    config(config) {
      if (config.build?.ssr) return;
      return {
        build: { rolldownOptions: { preserveEntrySignatures: 'allow-extension', output: { codeSplitting: { groups: [group] } } } }
      };
    },
    buildEnd() {
      publicModules = reach(this, (routeFile) => !consoleRoutes.test(routeFile));
    }
  };
}
