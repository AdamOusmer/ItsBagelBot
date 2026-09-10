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
// resolver of its own) and here it is the identity: adapters import only a
// relative stylesheet and their own siblings, so there is no bare specifier to
// rewrite. The moment one imports a package by name this has to become a real
// resolve against the importer's directory.
//
// Both plugins hand Bun `loader: "ts"`, not `"js"`, and for the same reason:
// neither compiler strips TypeScript. Svelte's leaves a `lang="ts"` block as
// source, @astrojs/compiler leaves the frontmatter alone (see the note on the
// astro plugin below), and Bun's js loader then dies on the first type
// annotation. Its transpiler erases them once told what it is being handed, and
// there is no cost for a file that happens to contain none.
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
      // loader "ts", not "js": @astrojs/compiler rewrites the TEMPLATE and
      // leaves the frontmatter alone, so a component that types its props with
      // `interface Props { … }` — which is how Astro documents props and how
      // every adapter here declares them — reaches the runner as TypeScript.
      // Bun's js loader dies on it with `Expected ";" but found "Props"`,
      // pointing at the .astro file, which reads like a compiler bug and is
      // not one. The ts loader strips the types the way Vite's astro plugin
      // does downstream.
      return { contents: code, loader: "ts" };
    });
  },
});

// Adapters import their own contract stylesheet (the CardAtmosphere pair set
// that precedent: the element and the CSS that makes it an element ship
// together, so a consumer imports one thing). Bun's runtime has no CSS loader,
// so without this the parity test dies on `import '../styles/elements/
// cursor.css'` before it renders anything.
//
// Stubbed to an empty module rather than parsed: this test compares MARKUP.
// What the CSS says is the contract's business and is asserted by the browser
// checks in the PR, not by a string diff here.
plugin({
  name: "css-stub",
  setup(build) {
    build.onLoad({ filter: /\.css$/ }, () => ({ contents: "", loader: "js" }));
  },
});
