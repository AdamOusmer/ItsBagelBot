// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { plugin } from "bun";
import { transform } from "@astrojs/compiler";
import { readFileSync } from "node:fs";
import { compile } from "svelte/compiler";

plugin({
  name: "svelte-ssr",
  setup(build) {
    build.onLoad({ filter: /\.svelte$/ }, ({ path }) => {
      const { js } = compile(readFileSync(path, "utf8"), {
        generate: "server",
        filename: path,
      });
      return { contents: js.code, loader: "ts" };
    });
  },
});

plugin({
  name: "astro-ssr",
  setup(build) {
    build.onLoad({ filter: /\.astro$/ }, async ({ path }) => {
      const { code } = await transform(readFileSync(path, "utf8"), {
        filename: path,
        resolvePath: async (specifier: string) => specifier,
      });
      return { contents: code, loader: "ts" };
    });
  },
});

plugin({
  name: "css-stub",
  setup(build) {
    build.onLoad({ filter: /\.css$/ }, () => ({ contents: "", loader: "js" }));
  },
});
