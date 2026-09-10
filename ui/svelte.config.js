// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// svelte-check refuses to run without this file, even for a package that has no
// preprocessors and no SvelteKit: it is how the tool finds the compiler options
// it should type-check against. `vitePreprocess` is deliberately absent —
// nothing in ui/svelte uses a preprocessor language, `lang="ts"` is handled by
// svelte-check itself, and adding the preprocessor would pull
// @sveltejs/vite-plugin-svelte in as a devDependency of a package that has no
// Vite build of its own. @bagel/ui ships its Svelte adapters as SOURCE —
// consumers resolve them through the `svelte` export condition and compile them
// in their own build — so anything configured here would silently apply to ui's
// type check and to nothing a consumer actually builds.
//
// `runes: true` is not set: it is the Svelte 5 default for a component using
// runes, and pinning it here would make an adapter that forgets $props() fail
// with a config error instead of the clearer runes error.
export default {};
