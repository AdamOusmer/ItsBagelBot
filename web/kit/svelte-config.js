// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { withBoundedCompress } from './scripts/bounded-compress.mjs';
import { withPrunedChunks } from './scripts/prune-server-chunks.mjs';

/** @type {NonNullable<NonNullable<import('@sveltejs/kit/vite').Config['csp']>['directives']>} */
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
 * @returns {import('@sveltejs/kit/vite').Config}
 */
export function consoleKitConfig({ directives = {} } = {}) {
  return {
    preprocess: vitePreprocess(),
    adapter: withPrunedChunks(withBoundedCompress(adapter({ precompress: true }))),
    alias: { $lib: 'src/lib' },
    // RootShell's post-deploy full reload needs pollInterval; without it navigation fetches deleted bundles.
    version: { name: process.env.BUILD_VERSION || 'dev', pollInterval: 60000 },
    paths: {
      relative: false
    },
    csp: {
      mode: 'auto',
      directives: { ...BASE_DIRECTIVES, ...directives }
    }
  };
}
