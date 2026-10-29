// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Keep this side-effect import: without it ARM and Intel images build different client bundles.
import '../sorted-readdir.mjs';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { stripDemoRoutes } from '../kit/scripts/strip-demo-routes.mjs';
import { localeChunks } from '../kit/scripts/locale-chunks.mjs';
import { i18nCatalogs } from '../kit/scripts/i18n-catalogs.ts';
import { i18nSlices } from '../kit/scripts/i18n-slices.ts';
import { bundledUiSheets } from '../kit/scripts/bundled-ui-sheets.mjs';
import { consoleKitConfig } from '../kit/svelte-config.js';

// Route files that import @bagel/kit/styles/ui-console.css.
const consoleRoutes = /\/src\/routes\/(\(app\)\/|welcome\/\+page\.svelte$|\+error\.svelte$)/;

export default defineConfig({
  plugins: [
    stripDemoRoutes(['/billing/demo-checkout/']),
    i18nCatalogs({ exclude: ['admin'], slices: true }),
    i18nSlices(),
    bundledUiSheets({ consoleRoutes }),
    sveltekit({
      ...consoleKitConfig({ directives: { 'frame-src': ['none'] } }),
      compilerOptions: { discloseVersion: false }
    })
  ],
  ssr: { noExternal: ['@bagel/kit', '@bagel/ui'], external: ['newrelic', 'iovalkey', 'pino'] },
  // Widening fs.allow to '..' or '../..' serves sibling apps or the whole repo over /@fs in dev.
  server: { port: 5173, fs: { allow: ['../kit', '../../ui', '../../locales'] } },
  // Named exports give every lazily imported locale file a namespace object: +40% gzip on the fr catalog.
  json: { namedExports: false },
  build: {
    minify: 'terser',
    sourcemap: false,
    terserOptions: { ecma: 2020, module: true, toplevel: true, compress: { passes: 2, ecma: 2020 } },
    // Without this group each locales/<code>/console file ships as its own chunk and request.
    rolldownOptions: { output: { codeSplitting: { groups: [localeChunks] } } }
  }
});
