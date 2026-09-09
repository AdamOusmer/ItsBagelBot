// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Bun test preload (registered in ../bunfig.toml): teach the test runner to
// import the two component formats this package ships adapters for.
//
// Both plugins deliberately use each framework's OWN compiler rather than a
// hand-written transform, because the point of the parity test is to compare
// what the frameworks actually emit. A shim that approximated either one would
// make the test pass on markup no browser ever receives.
//
// Svelte is compiled with `generate: 'server'`, which is the same output
// SvelteKit's SSR pass produces and the only one `render()` from svelte/server
// can call. `generate: 'client'` compiles without error and then fails inside
// the renderer with "component is not a function".
//
// Astro is compiled with @astrojs/compiler's `transform`, the same function the
// Astro Vite plugin calls. `resolvePath` is required (the compiler has no
// resolver of its own) and here it is the identity: fixtures import nothing, so
// there is no specifier to rewrite. The moment a fixture imports another
// component this has to become a real resolve against the importer's directory.
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
      return { contents: js.code, loader: "js" };
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
      return { contents: code, loader: "js" };
    });
  },
});
