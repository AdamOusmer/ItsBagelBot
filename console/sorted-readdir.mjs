// @ts-nocheck — deliberate monkey-patch of the fs overloads; typing it buys nothing.
// SvelteKit assigns client node IDs in the order returned by fs.readdirSync
// while walking src/routes (and builds the param-matcher list from src/params).
// Overlay filesystems can return a different order on the native ARM64 and
// x86_64 image builds, which makes their SSR HTML and client bundles
// incompatible even when they come from the same commit.
//
// Loaded via a side-effect import at the top of each app's vite.config.ts so
// the patch applies inside the build process itself. Do NOT rely on
// NODE_OPTIONS=--require for this: the images build with bun, and bun silently
// ignores --require in NODE_OPTIONS, which left the previous CJS shim inert
// and shipped mismatched per-arch bundles (immutable-asset 404s in production).
//
// Kit calls fs.readdirSync via the default `node:fs` import, so patching the
// module object's property intercepts every call under Node and bun alike.
import fs from 'node:fs';

const readdirSync = fs.readdirSync;

fs.readdirSync = function deterministicRouteRead(directory, options) {
  const entries = readdirSync.call(this, directory, options);
  const path = String(directory).replaceAll('\\', '/');

  // Sort all project-source reads (routes drive node IDs; params drive the
  // matcher list). node_modules and build outputs keep native order.
  if (!path.includes('/src/')) return entries;

  return entries.sort((a, b) => {
    const aName = typeof a === 'string' || Buffer.isBuffer(a) ? a : a.name;
    const bName = typeof b === 'string' || Buffer.isBuffer(b) ? b : b.name;
    return Buffer.from(aName).compare(Buffer.from(bName));
  });
};
