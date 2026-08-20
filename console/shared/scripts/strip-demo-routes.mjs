// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Build-level demo gate, the third of three, and the one for markup.
//
// assert-demo-gated.mjs proves the SOURCE can only produce a demo-free bundle;
// assert-production-clean.ts proves the emitted OUTPUT is demo-free. Both work
// by checking that demo code sits behind `const DEMO = dev && env.DEMO === '1'`,
// which folds away because `dev` is a build-time constant.
//
// A whole demo ROUTE cannot be gated that way. Its page component is not a
// branch inside a module, it IS the module: SvelteKit compiles it because the
// file exists, and its markup lands in the client bundle even when the route's
// server side 404s. The fake Tebex checkout screen was shipping to browsers on
// exactly that path — unreachable, and still a fake payment page in production.
//
// So the module is replaced with an empty component before the Svelte plugin
// compiles it. The route keeps existing (its `+page.server.ts` still 404s
// outside a demo build, which is what makes the URL a dead end); there is
// simply nothing left of the page to render or to read in a bundle.
//
/**
 * @param {string[]} routeMarkers path fragments identifying demo routes
 * @returns {import('vite').Plugin}
 */
export function stripDemoRoutes(routeMarkers) {
  // Same switch the app's own gate reads, so a demo build keeps its routes.
  const demoBuild = process.env.DEMO === '1';
  return {
    name: 'bagel-strip-demo-routes',
    // Ahead of the Svelte plugin: it must not see the real source.
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
