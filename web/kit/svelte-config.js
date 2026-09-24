// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

const BASE_DIRECTIVES = {
  'default-src': ['self'],
  'script-src': ['self', 'https://js-agent.newrelic.com'],
  'style-src': ['self'],
  'style-src-attr': ['unsafe-inline'],
  'font-src': ['self'],
  'img-src': ['self', 'data:', 'https://cdn.discordapp.com'],
  'connect-src': ['self', 'https://dashboard.itsbagelbot.com', 'https://*.nr-data.net'],
  'object-src': ['none'],
  'base-uri': ['self'],
  'frame-ancestors': ['none']
};

/**
 * @param {{ directives?: Record<string, string[]> }} [options]
 * @returns {import('@sveltejs/kit').Config}
 */
export function consoleKitConfig({ directives = {} } = {}) {
  return {
    preprocess: vitePreprocess(),
    kit: {
      adapter: adapter({ precompress: true }),
      // RootShell's post-deploy full reload needs pollInterval; without it navigation fetches deleted bundles.
      version: { name: process.env.BUILD_VERSION || 'dev', pollInterval: 60000 },
      paths: {
        relative: false
      },
      csp: {
        mode: 'auto',
        directives: { ...BASE_DIRECTIVES, ...directives }
      }
    }
  };
}
