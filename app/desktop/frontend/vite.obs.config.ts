// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';

export default defineConfig({
  root: 'obs',
  publicDir: '../public',
  resolve: { dedupe: ['svelte', '@luzir/bolota'] },
  plugins: [svelte({ configFile: false })],
  build: { outDir: '../obs-dist', emptyOutDir: true },
});
