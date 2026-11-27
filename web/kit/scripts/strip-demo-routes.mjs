// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const STRIPPED = {
  '+page.svelte': '<!-- dev-only route stripped from builds (kit/scripts/strip-demo-routes.mjs) -->',
  '+page.server.ts': "import { error } from '@sveltejs/kit';\nexport const load = () => error(404);\nexport const actions = {};\n"
};

/**
 * @param {string[]} routeMarkers
 * @returns {import('vite').Plugin}
 */
export function stripDemoRoutes(routeMarkers) {
  return {
    name: 'bagel-strip-demo-routes',
    enforce: 'pre',
    apply: 'build',
    /** @param {string} id */
    load(id) {
      const path = id.split('?')[0];
      if (!routeMarkers.some((marker) => path.includes(marker))) return null;
      const file = /** @type {keyof typeof STRIPPED} */ (path.split('/').pop());
      if (file.startsWith('+') && !(file in STRIPPED)) this.error(`${path}: dev-only route file has no build stub in strip-demo-routes.mjs`);
      return STRIPPED[file] ?? null;
    }
  };
}
