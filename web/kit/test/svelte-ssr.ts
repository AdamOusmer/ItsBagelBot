// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { plugin } from 'bun';
import { readFileSync } from 'node:fs';
import { compile } from 'svelte/compiler';

plugin({
  name: 'svelte-ssr',
  setup(build) {
    build.onLoad({ filter: /\.svelte$/ }, ({ path }) => {
      const { js } = compile(readFileSync(path, 'utf8'), { generate: 'server', filename: path });
      return { contents: js.code, loader: 'ts' };
    });
  },
});

plugin({
  name: 'css-stub',
  setup(build) {
    build.onLoad({ filter: /\.css$/ }, () => ({ contents: '', loader: 'js' }));
  },
});
