// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const uiGroup = {
  name: 'ui',
  test: /\/ui\/(svelte|lib|locales)\/|\/node_modules\/(svelte|esm-env|clsx)\//,
  entriesAware: true,
  entriesAwareMergeThreshold: 32 * 1024,
  priority: -1
};

// Client only: the same group on the server build produces a circular chunk.
/** @returns {import('vite').Plugin} */
export function clientUiChunks() {
  return {
    name: 'bagel-client-ui-chunks',
    config(config) {
      if (config.build?.ssr) return;
      return { build: { rolldownOptions: { output: { codeSplitting: { groups: [uiGroup] } } } } };
    }
  };
}
