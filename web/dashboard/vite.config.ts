// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Side-effect import: sorts src/ directory reads so the native ARM/Intel image
// builds assign identical SvelteKit node IDs and emit byte-identical client
// bundles. Must live here (inside the build process): bun ignores
// NODE_OPTIONS=--require, so a script-level shim never runs.
import '../sorted-readdir.mjs';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
// Lives with the other demo gates in web/kit, not here: see that file
// for why a demo ROUTE needs stripping rather than `dev`-gating.
import { stripDemoRoutes } from '../kit/scripts/strip-demo-routes.mjs';

export default defineConfig({
  plugins: [stripDemoRoutes(['/billing/demo-checkout/']), sveltekit()],
  // The shared package ships .svelte/.ts source; Vite must bundle (not externalize)
  // it for SSR so components compile. `newrelic` must stay external so it resolves
  // to the singleton preloaded via --import at runtime (bundling its native modules
  // + dynamic requires would break it and create a second, uninstrumented instance).
  // `iovalkey` (the Valkey read client) also stays external: ioredis-family clients
  // use dynamic requires that do not bundle cleanly for SSR.
  // `pino` stays external so the New Relic agent's require-hook wraps the real
  // module at runtime and local-decorates its log lines (bundling defeats the hook).
  ssr: { noExternal: ['@bagel/kit', '@bagel/ui'], external: ['newrelic', 'iovalkey', 'pino'] },
  // fs.allow: tokens.css lives in the workspace sibling web/kit and
  // @font-faces four woff2 files that now live in the design library, one
  // level further out again (repo-root ui/, linked as @bagel/ui). Vite
  // rewrites those url()s to /@fs/… absolute paths, and neither directory is
  // one SvelteKit's plugin allows (its own src, .svelte-kit, and the two
  // node_modules dirs), so each font answered 403, `document.fonts` reported
  // all four faces in `error`, and Syne 800 fell back to sans-serif, ~40%
  // narrower, which silently resized every width-sensitive layout in dev (the
  // /login hero grid measured a 547px title column against 902px in the Astro
  // original). Two scoped entries rather than one wide one: '..' would also
  // expose web/admin, web/marketing and web/docs over /@fs, and '../..' would
  // expose the whole repository including the Go tree. ../../ui is the real
  // directory, not the node_modules symlink, because Vite compares realpaths.
  // Dev only; production builds emit the fonts as hashed assets under
  // _app/immutable/assets.
  server: { port: 5173, fs: { allow: ['../kit', '../../ui'] } },
  build: {
    minify: 'terser'
  }
});
