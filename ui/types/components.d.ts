// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// `tsc --noEmit` (ui's `check` script) has no idea what a .svelte or a .astro
// file is; without these it reports "Cannot find module './fixture.svelte'" for
// the parity test and there is no tsconfig option that fixes it, because the
// modules genuinely do not exist until a compiler makes them.
//
// Typed loosely on purpose. The real type of a compiled Svelte component is
// svelte's `Component<Props>` and of a compiled Astro component is Astro's
// internal `AstroComponentFactory`; naming either here would make this package's
// type check depend on a framework, which is the one thing
// scripts/assert-framework-free.mjs exists to prevent. The parity test asserts
// on rendered HTML, so nothing of value is lost by the components themselves
// being opaque to tsc.
declare module '*.svelte' {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const component: any;
  export default component;
}

declare module '*.astro' {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const component: any;
  export default component;
}

// Same reason, one layer down: the adapters import their own contract
// stylesheet so a consumer cannot render a `.bb-btn` with no rules for it, and
// svelte-check reports a side-effect import of a .css file as a missing module
// ("Cannot find module or type declarations for side-effect import"). There is
// nothing to type; the declaration exists so the check has something to
// resolve.
declare module '*.css';
