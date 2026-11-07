// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { svelte } from '@sveltejs/vite-plugin-svelte';
import wails from '@wailsio/runtime/plugins/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    fs: { allow: ['.', '../../../ui', '../../../locales'] },
  },
  resolve: {
    // @bagel/ui resolves from its realpath and installs its own copies of these.
    dedupe: ['svelte', '@luzir/bolota'],
  },
  plugins: [svelte(), wails('./bindings')],
});
