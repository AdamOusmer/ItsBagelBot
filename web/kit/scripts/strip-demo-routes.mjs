// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * @param {string[]} routeMarkers
 * @returns {import('vite').Plugin}
 */
export function stripDemoRoutes(routeMarkers) {
  const demoBuild = process.env.DEMO === '1';
  return {
    name: 'bagel-strip-demo-routes',
    enforce: 'pre',
    /** @param {string} id */
    load(id) {
      if (demoBuild) return null;
      const path = id.split('?')[0];
      if (!path.endsWith('+page.svelte')) return null;
      if (!routeMarkers.some((marker) => path.includes(marker))) return null;
      return `<!-- demo route stripped from non-demo builds (shared/scripts/strip-demo-routes.mjs) -->`;
    }
  };
}
