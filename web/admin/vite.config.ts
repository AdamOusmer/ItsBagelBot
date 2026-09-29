// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Keep this side-effect import: without it ARM and Intel images build different client bundles.
import '../sorted-readdir.mjs';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { localeChunks } from '../kit/scripts/locale-chunks.mjs';

export default defineConfig({
  plugins: [sveltekit()],
  ssr: { noExternal: ['@bagel/kit', '@bagel/ui'], external: ['mysql2', 'newrelic', 'iovalkey', 'pino'] },
  // Widening fs.allow to '..' or '../..' serves sibling apps or the whole repo over /@fs in dev.
  server: { port: 5174, fs: { allow: ['../kit', '../../ui', '../../locales'] } },
  // Named exports give every lazily imported locale file a namespace object: +40% gzip on the fr catalog.
  json: { namedExports: false },
  build: {
    minify: 'terser',
    // Without this group each locales/<code>/console file ships as its own chunk and request.
    rolldownOptions: { output: { codeSplitting: { groups: [localeChunks] } } }
  }
});
