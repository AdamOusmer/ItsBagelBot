// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-check
import { defineConfig } from 'astro/config';

export default defineConfig({
  server: { host: '127.0.0.1', port: 4330 },
  devToolbar: { enabled: false },
  vite: { server: { fs: { allow: ['..'] } } },
});
